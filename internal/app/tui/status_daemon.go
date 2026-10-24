package tui

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// OpenStatus prepares status delivery for this picker. Daemon startup or reuse
// occurs on the first asynchronous read, never before entering the UI.
func (a *LiveAdapter) OpenStatus(ctx context.Context) (*statusDelivery, error) {
	a.mu.RLock()
	server, err := a.statusServer, a.statusErr
	a.mu.RUnlock()
	if err != nil || server.PID == 0 {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader := newDaemonReader(ctx, server)
	return &statusDelivery{ctx: ctx, adapter: a, readView: reader.readView, stop: reader.wait}, nil
}

const (
	statusStartupTimeout    = 5 * time.Second
	statusInitialRetryDelay = 2 * time.Second
	statusMaxRetryDelay     = 5 * time.Second
)

// errStatusServerGone marks a failed server check. The reader never looks for a
// replacement server, so it stops after this error.
var errStatusServerGone = errors.New("status tmux server is gone")

// subscription is the part of daemon.Subscription that the reader uses.
type subscription interface {
	Next() (daemon.View, error)
	Close() error
}

// daemonReader owns one subscription goroutine for a picker. The goroutine
// starts at the first read and stops when ctx ends. It keeps only the latest
// result, so a slow picker never queues old views.
type daemonReader struct {
	ctx    context.Context
	server observe.Server
	open   func(context.Context) (subscription, error)

	start sync.Once
	done  chan struct{}

	mu       sync.Mutex
	view     daemon.View
	err      error
	sequence uint64
	updated  chan struct{}
}

func newDaemonReader(ctx context.Context, server observe.Server) *daemonReader {
	r := &daemonReader{
		ctx:     ctx,
		server:  server,
		done:    make(chan struct{}),
		updated: make(chan struct{}),
	}
	r.open = r.connect
	return r
}

// readView waits for a result newer than after. It returns the result and its
// sequence number.
func (r *daemonReader) readView(ctx context.Context, after uint64) (daemon.View, uint64, error) {
	r.start.Do(func() {
		go r.run()
	})

	stopped := false
	for {
		r.mu.Lock()
		view, err, sequence, updated := r.view, r.err, r.sequence, r.updated
		r.mu.Unlock()

		// A stopped reader repeats its last result, so callers do not wait forever.
		if sequence != after || stopped {
			return view, sequence, err
		}

		select {
		case <-ctx.Done():
			return daemon.View{}, after, ctx.Err()
		case <-updated:
		case <-r.done:
			stopped = true
		}
	}
}

// wait blocks until the goroutine stops. The owner must cancel ctx first.
func (r *daemonReader) wait() {
	r.start.Do(func() {
		close(r.done)
	})
	<-r.done
}

func (r *daemonReader) run() {
	defer close(r.done)

	var delay time.Duration
	for r.ctx.Err() == nil {
		received, err := r.follow()
		if r.ctx.Err() != nil {
			return
		}

		r.publish(daemon.View{}, err)
		if errors.Is(err, errStatusServerGone) {
			return
		}

		if received {
			delay = 0
		}
		delay = min(max(delay*2, statusInitialRetryDelay), statusMaxRetryDelay)
		if !sleep(r.ctx, delay) {
			return
		}
	}
}

// follow connects, then publishes each view until the subscription ends. It
// reports whether at least one frame arrived.
func (r *daemonReader) follow() (bool, error) {
	ctx, cancel := context.WithTimeout(r.ctx, statusStartupTimeout)
	current, err := r.open(ctx)
	cancel()
	if err != nil {
		return false, err
	}
	defer current.Close()

	stop := context.AfterFunc(r.ctx, func() {
		_ = current.Close()
	})
	defer stop()

	received := false
	for {
		view, err := current.Next()
		if err != nil && !errors.Is(err, daemon.ErrUnavailable) {
			return received, err
		}

		received = true
		r.publish(view, err)
	}
}

func (r *daemonReader) connect(ctx context.Context) (subscription, error) {
	if err := r.server.Validate(ctx); err != nil {
		if ctx.Err() == nil {
			return nil, errors.Join(errStatusServerGone, err)
		}
		return nil, err
	}

	client, err := statusobserver.EnsureDaemon(ctx, r.server, "")
	if err != nil {
		return nil, err
	}

	return client.Subscribe(ctx)
}

func (r *daemonReader) publish(view daemon.View, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.view, r.err = view, err
	r.sequence++
	close(r.updated)
	r.updated = make(chan struct{})
}

func sleep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// bindStatusView uses the current projection and exact server incarnation;
// retained evidence never owns or copies the row index.
func (a *LiveAdapter) bindStatusView(view daemon.View) ui.StatusBindings {
	a.mu.RLock()
	defer a.mu.RUnlock()

	bindings, _ := a.bindStatusViewLocked(view)
	return bindings
}

func (a *LiveAdapter) bindStatusViewLocked(view daemon.View) (ui.StatusBindings, map[list.ItemID]status.PaneRef) {
	if a.statusErr != nil || a.statusServer.PID == 0 {
		return ui.StatusBindings{}, nil
	}

	server := a.statusServer.Identity()
	refs := make(map[string]status.PaneRef, view.Values.Len())
	for ref := range view.Values.All() {
		if ref.Server != server {
			return ui.StatusBindings{}, nil
		}
		refs[ref.ID] = ref
	}

	bindings := make(ui.StatusBindings, min(len(a.index), len(view.Locations)))
	var occurrences map[list.ItemID]status.PaneRef
	for _, location := range view.Locations {
		ref := refs[location.Pane]
		window := windowItemID(location.Session, location.Window)
		pane := destinationItemID(location.Session, location.Window, location.Pane)

		if item, ok := a.index[window]; ok && item.Kind == liveWindow {
			bindings[window] = append(bindings[window], ref)
			if occurrences == nil {
				occurrences = make(map[list.ItemID]status.PaneRef, len(view.Locations))
			}
			occurrences[pane] = ref
		}

		if item, ok := a.index[pane]; ok && item.Kind == liveDestination {
			bindings[pane] = append(bindings[pane], ref)
		}
	}

	return bindings, occurrences
}

func (a *LiveAdapter) bindStatusUpdate(view daemon.View, update *ui.StatusUpdate) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	bindings, occurrences := a.bindStatusViewLocked(view)
	update.Bindings = bindings
	if a.projection != ui.ViewTree || a.input == nil {
		update.Windows = make(map[list.ItemID][]ui.WindowPane)
		return
	}

	update.Windows = make(map[list.ItemID][]ui.WindowPane, len(a.index))
	home, _ := os.UserHomeDir()
	for _, session := range a.input.state.Sessions {
		for _, window := range session.Windows {
			id := windowItemID(session.ID, window.ID)
			panes := make([]ui.WindowPane, 0, len(window.Panes))

			for _, pane := range window.Panes {
				paneID := destinationItemID(session.ID, window.ID, pane.ID)
				panes = append(panes, ui.WindowPane{
					PaneID:  pane.ID,
					Ref:     occurrences[paneID],
					Active:  pane.Active,
					Details: treePaneDetails(session, a.index[id].Label, pane, home),
				})
			}

			update.Windows[id] = panes
		}
	}
}
