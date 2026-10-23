package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestWindowPreviewKeepsTheProjectedPaneAfterActivePaneChanges(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	window := &stub.state.Sessions[0].Windows[0]
	window.Panes = append(window.Panes, backend.Pane{ID: "%2", Path: "/other"})
	var captured backend.Target
	stub.captureHook = func(_ context.Context, target backend.Target) (string, error) {
		captured = target
		return "projected screen", nil
	}
	adapter := loadedAdapter(t, stub)
	window.Panes[0].Active, window.Panes[1].Active = false, true

	screen, err := adapter.Preview(t.Context(), ui.PreviewRequest{ItemID: windowItemID("$1", "@1")})

	require.NoError(t, err)
	require.Equal(t, "projected screen", screen)
	require.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"}, captured)
	require.Equal(t, 1, stub.queryCalls, "preview does not refresh the projection")
	require.Empty(t, stub.switchCalls)
}

func TestServicePreviewAndOpenResolveTheSameTarget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		projection projectionKind
		id         list.ItemID
		want       backend.Target
	}{
		{"displayed tree pane", projectionTree, windowItemID("$1", "@1"), backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%2"}},
		{"flat ignores override", projectionFlat, destinationItemID("$1", "@1", "%1"), backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"}},
		{"session ignores override", projectionTree, sessionItemID("$1"), backend.Target{SessionID: "$1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubBackend{state: liveState()}
			window := &stub.state.Sessions[0].Windows[0]
			window.Panes = append(window.Panes, backend.Pane{ID: "%2", Path: "/agent"})
			var targets []backend.Target
			stub.captureHook = func(_ context.Context, target backend.Target) (string, error) {
				targets = append(targets, target)
				return "screen", nil
			}
			adapter := testAdapter(stub)
			adapter.projection = tc.projection
			service := Service{Driver: adapter,
				RunUI: func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, preview ui.PreviewOptions, _ ui.StatusOptions) (list.ItemID, error) {
					screen, err := preview.Read(ctx, ui.PreviewRequest{ItemID: tc.id, PaneID: "%2"})
					require.NoError(t, err)
					require.Equal(t, "screen", screen)
					opened, err := dispatch(ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: tc.id, PaneID: "%2"})
					return opened.Navigation, err
				},
			}

			err := service.Run(t.Context())

			require.NoError(t, err)
			require.Equal(t, []backend.Target{tc.want}, targets)
			require.Equal(t, []backend.Target{tc.want}, stub.switchCalls)
			require.Empty(t, stub.applyCalls)
		})
	}
}

func TestPreviewReportsCaptureFailureWithoutFallingBack(t *testing.T) {
	captureErr := errors.New("pane no longer belongs to the window")
	var targets []backend.Target
	stub := &stubBackend{state: liveState(), captureHook: func(_ context.Context, target backend.Target) (string, error) {
		targets = append(targets, target)
		return "", captureErr
	}}
	adapter := loadedAdapter(t, stub)

	screen, err := adapter.Preview(t.Context(), ui.PreviewRequest{ItemID: windowItemID("$1", "@1"), PaneID: "%removed"})

	require.ErrorIs(t, err, captureErr)
	require.Empty(t, screen)
	require.Equal(t, []backend.Target{{SessionID: "$1", WindowID: "@1", PaneID: "%removed"}}, targets)
	require.Equal(t, 1, stub.queryCalls)
	require.Empty(t, stub.switchCalls)
}
