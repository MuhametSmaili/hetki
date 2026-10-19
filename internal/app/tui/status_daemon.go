package tui

import (
	"context"
	"os"
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
	reader := &daemonReader{server: server, gate: make(chan struct{}, 1)}
	return &statusDelivery{ctx: ctx, adapter: a, readView: reader.readView}, nil
}

const (
	statusStartupTimeout    = 5 * time.Second
	statusInitialRetryDelay = 2 * time.Second
	statusMaxRetryDelay     = 5 * time.Second
)

type daemonReader struct {
	server observe.Server
	gate   chan struct{} // Serializes reads and recovery; waiting callers can cancel.

	client     *daemon.Client
	watch      bool
	terminal   error
	err        error
	retryAt    time.Time
	retryDelay time.Duration
}

func (r *daemonReader) connect(ctx context.Context) error {
	if r.client != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, statusStartupTimeout)
	defer cancel()

	if err := r.server.Validate(ctx); err != nil {
		if ctx.Err() == nil {
			// Uncertain native identity is terminal; never discover a replacement.
			r.terminal = err
		}
		return err
	}
	client, err := statusobserver.EnsureDaemon(ctx, r.server, "")
	if err != nil {
		return err
	}
	watch, err := client.SupportsWatch(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.client, r.watch = client, watch
	return nil
}

// readView returns failures before recovery so the delivery owner can invalidate
// evidence. Retry eligibility is checked by the existing read loop, not a worker.
func (r *daemonReader) readView(ctx context.Context, after daemon.Cursor) (daemon.View, error) {
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	case <-ctx.Done():
		return daemon.View{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return daemon.View{}, err
	}
	if r.terminal != nil {
		return daemon.View{}, r.terminal
	}
	if time.Now().Before(r.retryAt) {
		return daemon.View{}, r.err
	}

	connecting := r.client == nil
	view, err := r.attempt(ctx, after)
	if contextErr := ctx.Err(); contextErr != nil {
		if connecting {
			r.failed(contextErr)
		}
		return daemon.View{}, contextErr
	}
	if err != nil {
		r.failed(err)
		return daemon.View{}, err
	}

	r.err = nil
	r.retryAt = time.Time{}
	r.retryDelay = 0
	return view, nil
}

func (r *daemonReader) attempt(ctx context.Context, after daemon.Cursor) (daemon.View, error) {
	if err := r.connect(ctx); err != nil {
		return daemon.View{}, err
	}
	if r.watch {
		return r.client.WatchView(ctx, after)
	}
	// Older development daemons keep ordinary views and the UI polling cadence.
	return r.client.ReadView(ctx)
}

func (r *daemonReader) failed(err error) {
	r.client = nil
	r.watch = false
	r.err = err
	r.retryDelay = min(max(r.retryDelay*2, statusInitialRetryDelay), statusMaxRetryDelay)
	r.retryAt = time.Now().Add(r.retryDelay)
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
	if a.projection != projectionTree || a.input == nil {
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
					Ref: occurrences[paneID], Active: pane.Active,
					Details: treePaneDetails(session, a.index[id].Label, pane, home),
				})
			}

			update.Windows[id] = panes
		}
	}
}
