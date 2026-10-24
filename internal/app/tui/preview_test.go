package tui

import (
	"context"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func (*switchDriver) Preview(context.Context, ui.PreviewRequest) (string, error) {
	panic("unexpected preview")
}

func (*navigationDriver) Preview(context.Context, ui.PreviewRequest) (string, error) {
	panic("unexpected preview")
}

func (d *blockingDriver) Preview(ctx context.Context, _ ui.PreviewRequest) (string, error) {
	_, err := d.Execute(ctx, ui.ActionRequest{})
	return "", err
}

func TestPreviewResolvesOnlyTheSelectedTarget(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	var targets []backend.Target
	stub.captureHook = func(_ context.Context, target backend.Target) (string, error) {
		targets = append(targets, target)
		return "screen", nil
	}
	adapter := loadedAdapter(t, stub)
	for _, id := range []list.ItemID{sessionItemID("$1"), windowItemID("$1", "@1")} {
		text, err := adapter.Preview(context.Background(), ui.PreviewRequest{ItemID: id})
		require.NoError(t, err)
		require.Equal(t, "screen", text)
	}
	require.Equal(t, []backend.Target{{SessionID: "$1"}, {SessionID: "$1", WindowID: "@1", PaneID: "%1"}}, targets)
	require.Equal(t, 1, stub.queryCalls, "preview must not reload state")
	require.Empty(t, stub.applyCalls)
	require.Empty(t, stub.switchCalls)
	require.Nil(t, adapter.pendingNavigation)

	// Same-path panes must preview precisely the selected pane.
	state := liveState()
	window := &state.Sessions[0].Windows[0]
	window.Panes = append(window.Panes, window.Panes[0])
	window.Panes[0].ID, window.Panes[0].Active = "%0", false
	stub.state = state
	_, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionRefresh})
	require.NoError(t, err)
	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection, ItemID: windowItemID("$1", "@1")})
	require.NoError(t, err)
	id := destinationItemID("$1", "@1", "%1")
	_, err = adapter.Preview(context.Background(), ui.PreviewRequest{ItemID: id})
	require.NoError(t, err)
	require.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"}, targets[len(targets)-1])
	require.Equal(t, adapter.index[id].Target, targets[len(targets)-1])
	require.Equal(t, 2, stub.queryCalls)

	_, err = adapter.Preview(context.Background(), ui.PreviewRequest{ItemID: list.ItemID{ID: "removed"}})
	require.ErrorContains(t, err, "stale")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = adapter.Preview(ctx, ui.PreviewRequest{ItemID: id})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, targets, 3)
}

func TestPreviewDoesNotHoldTheIndexLockDuringCapture(t *testing.T) {
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	stub := &stubBackend{state: liveState(), captureHook: func(context.Context, backend.Target) (string, error) {
		close(started)
		<-release
		return "old screen", nil
	}}
	adapter := loadedAdapter(t, stub)
	go func() {
		_, err := adapter.Preview(context.Background(), ui.PreviewRequest{ItemID: windowItemID("$1", "@1")})
		done <- err
	}()
	awaitChannel(t, started)
	refreshed := make(chan error, 1)
	go func() {
		_, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionRefresh})
		refreshed <- err
	}()
	err := awaitChannel(t, refreshed)
	close(release)
	require.NoError(t, err)
	require.NoError(t, awaitChannel(t, done))
}

func TestPreviewReadCanRaceWithSnapshotReplacement(t *testing.T) {
	stub := &stubBackend{state: liveState(), captureHook: func(context.Context, backend.Target) (string, error) { return "", nil }}
	adapter := loadedAdapter(t, stub)
	done := make(chan error, 1)
	go func() {
		for range 100 {
			if _, err := adapter.Preview(context.Background(), ui.PreviewRequest{ItemID: windowItemID("$1", "@1")}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for range 100 {
		_, err := adapter.Load(context.Background(), ui.ViewTree)
		require.NoError(t, err)
	}
	require.NoError(t, awaitChannel(t, done))
}

func TestServiceCancelsAndJoinsPreviewOnExit(t *testing.T) {
	driver := &blockingDriver{started: make(chan struct{}), stopped: make(chan struct{})}
	done := make(chan error, 1)
	var read ui.PreviewFunc
	options := ui.DefaultOptions()
	options.PreviewWidth = 65
	service := Service{Options: options, Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
			require.Equal(t, 65, options.PreviewWidth)
			read = runtime.Preview
			go func() {
				_, err := read(context.Background(), ui.PreviewRequest{ItemID: list.ItemID{ID: "pane"}})
				done <- err
			}()
			awaitChannel(t, driver.started)
			return ui.Result{}, nil
		},
	}
	require.NoError(t, service.Run(context.Background()))
	select {
	case <-driver.stopped:
	default:
		t.Fatal("preview outlived UI")
	}
	require.ErrorIs(t, awaitChannel(t, done), context.Canceled)
	_, err := read(context.Background(), ui.PreviewRequest{ItemID: list.ItemID{ID: "late"}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestServicePropagatesSelectionCancellation(t *testing.T) {
	driver := &blockingDriver{started: make(chan struct{}), stopped: make(chan struct{})}
	done := make(chan error, 1)
	service := Service{Options: ui.DefaultOptions(), Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				_, err := runtime.Preview(ctx, ui.PreviewRequest{ItemID: list.ItemID{ID: "pane"}})
				done <- err
			}()
			awaitChannel(t, driver.started)
			cancel()
			require.ErrorIs(t, awaitChannel(t, done), context.Canceled)
			return ui.Result{}, nil
		},
	}
	require.NoError(t, service.Run(context.Background()))
}

func TestServiceRejectsPreviewWidthBeforeLoading(t *testing.T) {
	for _, width := range []int{-1, 100} {
		driver := &switchDriver{}
		options := ui.DefaultOptions()
		options.PreviewWidth = width
		err := (Service{Options: options, Driver: driver}).Run(context.Background())
		require.ErrorContains(t, err, "preview width")
		require.Zero(t, driver.loads)
	}
}
