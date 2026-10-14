package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func awaitChannel[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for test channel")
		var zero T
		return zero
	}
}

type switchDriver struct {
	loads       int
	navigations []list.ItemID
	uiReturned  bool
}

func (d *switchDriver) Load(context.Context) (list.Snapshot, error) {
	d.loads++
	return list.Snapshot{}, nil
}

func (d *switchDriver) Execute(context.Context, ui.ActionRequest) (ui.ActionResult, error) {
	return ui.ActionResult{Navigation: list.ItemID{ID: "dev"}}, nil
}

func (d *switchDriver) Navigate(_ context.Context, outcome list.ItemID) error {
	if !d.uiReturned {
		return assert.AnError
	}
	d.navigations = append(d.navigations, outcome)
	return nil
}

type blockingDriver struct {
	started chan struct{}
	stopped chan struct{}
}

func (d *blockingDriver) Load(context.Context) (list.Snapshot, error) {
	return list.Snapshot{}, nil
}

func (d *blockingDriver) Execute(ctx context.Context, _ ui.ActionRequest) (ui.ActionResult, error) {
	close(d.started)
	<-ctx.Done()
	close(d.stopped)
	return ui.ActionResult{}, ctx.Err()
}

func (d *blockingDriver) Navigate(context.Context, list.ItemID) error { return nil }

func TestServiceUsesConfiguredStartMode(t *testing.T) {
	for _, test := range []struct {
		configured ui.StartMode
		want       ui.StartMode
	}{
		{want: ui.StartModeFilter},
		{configured: ui.StartModeNormal, want: ui.StartModeNormal},
		{configured: ui.StartModeJump, want: ui.StartModeJump},
	} {
		var got ui.StartMode
		service := Service{
			Driver:    &switchDriver{},
			StartMode: test.configured,
			RunUI: func(_ context.Context, _ list.Snapshot, _ ui.KeyMap, startMode ui.StartMode, _ ui.DispatchFunc, preview ui.PreviewOptions, status ui.StatusOptions) (list.ItemID, error) {
				require.Equal(t, ui.DefaultPreviewWidth, preview.Width)
				require.Nil(t, status.Read)
				got = startMode
				return list.ItemID{}, nil
			},
		}
		require.NoError(t, service.Run(context.Background()))
		require.Equal(t, test.want, got)
	}
}

func TestUIExitCancelsAndJoinsBusyEffect(t *testing.T) {
	driver := &blockingDriver{started: make(chan struct{}), stopped: make(chan struct{})}
	dispatchDone := make(chan error, 1)
	service := Service{
		Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, _ ui.StatusOptions) (list.ItemID, error) {
			go func() {
				_, err := dispatch(ui.ActionRequest{ActionID: ui.ActionCreateSession})
				dispatchDone <- err
			}()
			awaitChannel(t, driver.started)
			return list.ItemID{}, nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, service.Run(ctx))
	select {
	case <-driver.stopped:
	default:
		t.Fatal("busy effect outlived TUI run")
	}
	require.ErrorIs(t, awaitChannel(t, dispatchDone), context.Canceled)
}

type navigationDriver struct {
	started chan struct{}
}

func (d *navigationDriver) Load(context.Context) (list.Snapshot, error) {
	return list.Snapshot{}, nil
}
func (d *navigationDriver) Execute(context.Context, ui.ActionRequest) (ui.ActionResult, error) {
	return ui.ActionResult{}, nil
}
func (d *navigationDriver) Navigate(ctx context.Context, _ list.ItemID) error {
	close(d.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestRunPreservesParentCancellation(t *testing.T) {
	started := make(chan struct{})
	service := Service{
		Driver: &switchDriver{},
		RunUI: func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, _ ui.StatusOptions) (list.ItemID, error) {
			close(started)
			<-ctx.Done()
			return list.ItemID{}, tea.ErrProgramKilled
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()

	awaitChannel(t, started)
	cancel()

	require.ErrorIs(t, awaitChannel(t, done), context.Canceled)
}

func TestPostExitNavigationUsesParentCancellation(t *testing.T) {
	driver := &navigationDriver{started: make(chan struct{})}
	service := Service{
		Driver: driver,
		RunUI: func(context.Context, list.Snapshot, ui.KeyMap, ui.StartMode, ui.DispatchFunc, ui.PreviewOptions, ui.StatusOptions) (list.ItemID, error) {
			return list.ItemID{ID: "dev"}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()

	awaitChannel(t, driver.started)
	cancel()

	require.ErrorIs(t, awaitChannel(t, done), context.Canceled)
}

func TestSuccessfulSwitchNavigatesAfterUIWithoutRefresh(t *testing.T) {
	driver := &switchDriver{}
	service := Service{
		Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, _ ui.StatusOptions) (list.ItemID, error) {
			defer func() { driver.uiReturned = true }()
			result, err := dispatch(ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: list.ItemID{ID: "dev"}})
			return result.Navigation, err
		},
	}

	require.NoError(t, service.Run(context.Background()))
	assert.Equal(t, 1, driver.loads)
	assert.Equal(t, []list.ItemID{{ID: "dev"}}, driver.navigations)
}
