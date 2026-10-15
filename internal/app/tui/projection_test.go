package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

type projectionStatusBackend struct {
	stubBackend
	server     observe.Server
	statusErr  error
	afterQuery func()
}

func (b *projectionStatusBackend) QueryStatusState(context.Context) (backend.StateResult, observe.Server, error) {
	b.queryCalls++
	if b.afterQuery != nil {
		b.afterQuery()
	}
	return b.state, b.server, b.statusErr
}

func TestToggleDistinguishesUnloadedAndEmptyState(t *testing.T) {
	b := &stubBackend{}
	adapter := testAdapter(b)

	_, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})

	require.ErrorContains(t, err, "live state is not loaded")
	require.Zero(t, b.queryCalls)

	_, err = adapter.Load(t.Context())
	require.NoError(t, err)
	for range 2 {
		result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})
		require.NoError(t, err)
		require.Empty(t, result.Snapshot.Items)
	}
	require.Equal(t, 1, b.queryCalls, "empty loaded inventory is still a usable local input")
}

func TestInvalidRefreshPreservesProjectionInputAndStatusMetadata(t *testing.T) {
	server := observe.Server{PID: 123, Birth: "original", Started: 1}
	state := liveState()
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		{Server: server.Identity(), ID: "%1", Generation: "pane"}: {State: status.Unknown},
	})
	require.NoError(t, err)
	view := daemon.View{Values: values, Locations: []daemon.Location{{Session: "$1", Window: "@1", Pane: "%1"}}}
	b := &projectionStatusBackend{stubBackend: stubBackend{state: state}, server: server}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	initial, err := adapter.Load(t.Context())
	require.NoError(t, err)
	bindings := adapter.bindStatusView(view)
	require.NotEmpty(t, bindings)
	// A successful fallback query must not publish its status error if projection fails.
	b.statusErr = observe.ErrServerChanged
	b.state.Sessions = append(b.state.Sessions, b.state.Sessions[0])

	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionRefresh, ItemID: initial.Items[0].ID})

	require.Error(t, err)
	require.Equal(t, server, adapter.statusServer)
	require.Equal(t, bindings, adapter.bindStatusView(view))
	reader, statusErr := adapter.OpenStatus(t.Context())
	require.NoError(t, statusErr)
	require.NotNil(t, reader)

	result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})

	require.NoError(t, err)
	require.Len(t, result.Snapshot.Items, 1, "failed refresh must not replace the cached input")
	require.Equal(t, "core", result.Snapshot.Items[0].Primary)
	require.Equal(t, server, adapter.statusServer)
	require.Equal(t, 3, b.queryCalls, "initial status query, then failed refresh with fallback; no toggle query")
}

func TestCanceledSnapshotPreparationPreservesLastGoodState(t *testing.T) {
	for _, operation := range []string{"load", "refresh", "toggle"} {
		t.Run(operation, func(t *testing.T) {
			statusFailure := errors.New("status unavailable")
			b := &projectionStatusBackend{stubBackend: stubBackend{state: liveState()}, statusErr: statusFailure}
			adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
			initial, err := adapter.Load(t.Context())
			require.NoError(t, err)
			b.server = observe.Server{PID: 123, Birth: "replacement", Started: 2}
			b.statusErr = nil
			b.state = backend.StateResult{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			b.afterQuery = cancel

			switch operation {
			case "load":
				_, err = adapter.Load(ctx)
			case "refresh":
				_, err = adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionRefresh})
			case "toggle":
				cancel()
				// Exercise publication rollback rather than Execute's early cancellation guard.
				_, err = adapter.toggleProjection(ctx, initial.Items[0].ID)
			}

			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, projectionFlat, adapter.projection)
			require.Zero(t, adapter.statusServer)
			reader, statusErr := adapter.OpenStatus(t.Context())
			require.Nil(t, reader)
			require.ErrorIs(t, statusErr, statusFailure)
			menu, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionContextMenu, ItemID: initial.Items[0].ID})
			require.NoError(t, err)
			require.NotNil(t, menu.Menu)

			queries := b.queryCalls
			rebound, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})
			require.NoError(t, err)
			require.Len(t, rebound.Snapshot.Items, 1)
			require.Equal(t, "core", rebound.Snapshot.Items[0].Primary)
			require.Zero(t, adapter.statusServer)
			require.ErrorIs(t, adapter.statusErr, statusFailure)
			require.Equal(t, queries, b.queryCalls)
		})
	}
}
