package tui

import (
	"context"
	"sync"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
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
	reader := &daemonReader{server: server}
	return &statusDelivery{ctx: ctx, adapter: a, readView: reader.readView}, nil
}

type daemonReader struct {
	server observe.Server
	once   sync.Once
	client *daemon.Client
	err    error
	watch  bool
	mu     sync.Mutex
	cursor daemon.Cursor
}

func (r *daemonReader) connect(ctx context.Context) error {
	r.once.Do(func() {
		r.client, r.err = statusobserver.EnsureDaemon(ctx, r.server, "")
		if r.err != nil {
			return
		}

		r.watch, r.err = r.client.SupportsWatch(ctx)
	})
	return r.err
}

// readView retains the last delivered cursor. Old development daemons that do
// not advertise watches keep using ordinary reads and the UI's polling cadence.
func (r *daemonReader) readView(ctx context.Context) (daemon.View, error) {
	if err := r.connect(ctx); err != nil {
		return daemon.View{}, err
	}

	if !r.watch {
		return r.client.ReadView(ctx)
	}

	r.mu.Lock()
	after := r.cursor
	r.mu.Unlock()

	view, err := r.client.WatchView(ctx, after)

	r.mu.Lock()
	if r.cursor == after {
		r.cursor = view.Cursor
	}
	r.mu.Unlock()

	return view, err
}

// bindStatusView uses the current projection and exact server incarnation;
// retained evidence never owns or copies the row index.
func (a *LiveAdapter) bindStatusView(view daemon.View) ui.StatusBindings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	bindings := make(ui.StatusBindings)
	if a.statusErr != nil || a.statusServer.PID == 0 {
		return bindings
	}
	server := a.statusServer.Identity()
	refs := make(map[string]status.PaneRef, view.Values.Len())
	for ref := range view.Values.All() {
		if ref.Server != server {
			return bindings
		}
		refs[ref.ID] = ref
	}
	for _, location := range view.Locations {
		ref := refs[location.Pane]
		window := windowItemID(location.Session, location.Window)
		if item, ok := a.index[window]; ok && item.Kind == liveWindow {
			bindings[window] = append(bindings[window], ref)
		}
		pane := destinationItemID(location.Session, location.Window, location.Pane)
		if item, ok := a.index[pane]; ok && item.Kind == liveDestination {
			bindings[pane] = append(bindings[pane], ref)
		}
	}
	return bindings
}
