package tui

import (
	"context"
	"sync"

	"github.com/MSmaili/hetki/internal/app/statusobserver"
	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/daemon"
	ui "github.com/MSmaili/hetki/internal/tui"
)

// OpenStatus prepares a lazy reader for the loaded projection. Daemon startup or
// reuse occurs on the first asynchronous read, never before entering the UI.
// The reader owns no workers or persistent connections.
func (a *LiveAdapter) OpenStatus(ctx context.Context) (StatusReader, error) {
	a.mu.RLock()
	server, err := a.statusServer, a.statusErr
	a.mu.RUnlock()
	if err != nil || server.PID == 0 {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &daemonReader{server: server}, nil
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

func (r *daemonReader) ReadStatus(ctx context.Context) (status.Snapshot, error) {
	if err := r.connect(ctx); err != nil {
		return status.Snapshot{}, err
	}
	return r.client.ReadStatus(ctx)
}

func (r *daemonReader) ReadView(ctx context.Context) (daemon.View, error) {
	if err := r.connect(ctx); err != nil {
		return daemon.View{}, err
	}
	return r.client.ReadView(ctx)
}

// WatchView retains the last delivered cursor. Old development daemons that do
// not advertise watches keep using ordinary reads and the UI's polling cadence.
func (r *daemonReader) WatchView(ctx context.Context) (daemon.View, error) {
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
