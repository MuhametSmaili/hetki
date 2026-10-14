package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

type projectionStatusBackend struct {
	stubBackend
	server     observe.Server
	statusErr  error
	afterQuery func()
}

func (b *projectionStatusBackend) QueryStatusState(context.Context) (backend.StateResult, observe.Server, error) {
	if b.afterQuery != nil {
		b.afterQuery()
	}
	return b.state, b.server, b.statusErr
}

func TestInvalidProjectionPreservesStatusMetadata(t *testing.T) {
	server := observe.Server{PID: 123, Birth: "original", Started: 1}
	state := liveState()
	state.Sessions[0].Windows[0].Panes[0].StatusRef = status.PaneRef{Server: server.Identity(), ID: "%1", Generation: "pane"}
	b := &projectionStatusBackend{stubBackend: stubBackend{state: state}, server: server}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	initial, err := adapter.Load(t.Context())
	require.NoError(t, err)
	bindings := adapter.StatusBindings()
	require.NotEmpty(t, bindings)
	// A successful fallback query must not publish its status error if projection fails.
	b.statusErr = observe.ErrServerChanged
	b.state.Sessions = append(b.state.Sessions, b.state.Sessions[0])

	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection, ItemID: initial.Items[0].ID})

	require.Error(t, err)
	require.Equal(t, server, adapter.statusServer)
	require.Equal(t, bindings, adapter.StatusBindings())
	reader, statusErr := adapter.OpenStatus(t.Context())
	require.NoError(t, statusErr)
	require.NotNil(t, reader)
}

func TestCanceledSnapshotPreparationPreservesLastGoodState(t *testing.T) {
	for _, operation := range []string{"load", "toggle"} {
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

			if operation == "toggle" {
				_, err = adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionToggleProjection, ItemID: initial.Items[0].ID})
			} else {
				_, err = adapter.Load(ctx)
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
		})
	}
}
