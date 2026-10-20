package tui

import (
	"context"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func linkedPaneState() backend.StateResult {
	state := liveState()
	window := &state.Sessions[0].Windows[0]
	window.Panes = append(window.Panes, backend.Pane{
		ID: "%2", Index: 1, Path: "/work",
	})
	state.Sessions = append(state.Sessions, backend.Session{ID: "$2", Name: "linked", Windows: []backend.Window{*window}})
	return state
}

type statusTestDriver struct {
	*LiveAdapter
	read func(context.Context) (daemon.View, error)
}

func (d statusTestDriver) OpenStatus(ctx context.Context) (*statusDelivery, error) {
	return &statusDelivery{ctx: ctx, adapter: d.LiveAdapter, readView: func(ctx context.Context, after uint64) (daemon.View, uint64, error) {
		view, err := d.read(ctx)
		return view, after + 1, err
	}}, nil
}

func statusDeliveryFixture(t *testing.T) (*LiveAdapter, daemon.View) {
	t.Helper()
	now := time.Now()
	server := observe.Server{PID: 123, Birth: "fixture", Started: 1}
	b := &projectionStatusBackend{stubBackend: stubBackend{state: liveState()}, server: server}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	adapter.projection = projectionTree
	pane := status.PaneRef{Server: server.Identity(), ID: "%1", Generation: "birth"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Blocked, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	return adapter, daemon.View{Values: values, Locations: []daemon.Location{{Session: "$1", Window: "@1", Pane: "%1"}}}
}

func TestServiceSuppliesBindingsOnProjectionReplacement(t *testing.T) {
	adapter, view := statusDeliveryFixture(t)
	now := time.Now()
	reads := 0
	service := Service{Driver: statusTestDriver{LiveAdapter: adapter, read: func(context.Context) (daemon.View, error) {
		reads++
		return view, nil
	}}}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		require.Empty(t, options.Bindings, "ordinary backend rows have no status references")
		initial := options.Read(ctx, 0)
		require.NoError(t, initial.Err)
		require.Equal(t, status.Blocked, initial.Bindings.StateAt(windowItemID("$1", "@1"), initial.Values, now))

		for _, id := range []list.ItemID{destinationItemID("$1", "@1", "%1"), windowItemID("$1", "@1")} {
			result, err := dispatch(ui.ActionRequest{ActionID: ui.ActionToggleProjection})
			require.NoError(t, err)
			require.NotNil(t, result.Snapshot)
			require.NotNil(t, result.Status)
			require.Equal(t, status.Blocked, result.Status.Bindings.StateAt(id, result.Status.Values, now))
		}
		require.Equal(t, 1, reads, "replacement must not depend on another transport read")
		return list.ItemID{}, nil
	}

	err := service.Run(t.Context())

	require.NoError(t, err)
}

func TestServiceCancelsAndJoinsStatusOnExit(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	done := make(chan ui.StatusUpdate, 1)
	adapter, _ := statusDeliveryFixture(t)
	calls := 0
	var read func(context.Context, uint64) ui.StatusUpdate
	service := Service{Driver: statusTestDriver{LiveAdapter: adapter, read: func(ctx context.Context) (daemon.View, error) {
		calls++
		close(started)
		<-ctx.Done()
		close(stopped)
		return daemon.View{}, ctx.Err()
	}}, RunUI: func(_ context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		read = options.Read
		go func() { done <- read(context.Background(), 0) }()
		awaitChannel(t, started)
		return list.ItemID{}, nil
	}}

	err := service.Run(context.Background())
	late := read(context.Background(), 0)

	require.NoError(t, err)
	select {
	case <-stopped:
	default:
		t.Fatal("status reader outlived UI")
	}
	require.NoError(t, awaitChannel(t, done).Err, "picker exit stays quiet")
	require.NoError(t, late.Err, "picker cancellation stays quiet")
	require.Equal(t, 1, calls, "callbacks after exit must not read")
}
