package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
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
	view        ui.ViewMode
	navigations []list.ItemID
	uiReturned  bool
}

func (d *switchDriver) Load(_ context.Context, view ui.ViewMode) (list.Snapshot, error) {
	d.loads++
	d.view = view
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

func (d *blockingDriver) Load(context.Context, ui.ViewMode) (list.Snapshot, error) {
	return list.Snapshot{}, nil
}

func (d *blockingDriver) Execute(ctx context.Context, _ ui.ActionRequest) (ui.ActionResult, error) {
	close(d.started)
	<-ctx.Done()
	close(d.stopped)
	return ui.ActionResult{}, ctx.Err()
}

func (d *blockingDriver) Navigate(context.Context, list.ItemID) error { return nil }

func TestServicePassesResolvedOptionsAndInitialView(t *testing.T) {
	options := ui.DefaultOptions()
	options.Initial = ui.State{View: ui.ViewTree, Scope: ui.ScopeTracked, Preview: ui.PreviewOn}
	options.StartMode, options.PreviewWidth = ui.StartModeNormal, 45
	keys, err := ui.ResolveKeyMap(map[ui.KeyMode][]ui.Binding{
		ui.KeyModeNormal: {{Action: ui.ActionQuit, Keys: []string{"Q"}}},
	})
	require.NoError(t, err)
	options.Keys = keys
	driver := &switchDriver{}
	service := Service{Driver: driver, Options: options,
		RunUI: func(_ context.Context, _ list.Snapshot, got ui.Options, runtime ui.Runtime) (ui.Result, error) {
			require.Equal(t, options, got)
			require.Equal(t, ui.ViewTree, driver.view)
			require.NotNil(t, runtime.Dispatch)
			require.NotNil(t, runtime.Preview)
			return ui.Result{}, nil
		},
	}

	require.NoError(t, service.Run(t.Context()))
	require.Equal(t, 1, driver.loads)
	require.Equal(t, ui.DefaultOptions(), NewService(nil).Options)
}

func TestServiceRejectsInvalidOptionsBeforeLoadOrUI(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*ui.Options)
	}{
		{"zero options", func(o *ui.Options) { *o = ui.Options{} }},
		{"invalid view", func(o *ui.Options) { o.Initial.View = "invalid" }},
		{"invalid scope", func(o *ui.Options) { o.Initial.Scope = "invalid" }},
		{"invalid preview", func(o *ui.Options) { o.Initial.Preview = "invalid" }},
		{"missing keys", func(o *ui.Options) { o.Keys = ui.KeyMap{} }},
		{"invalid start mode", func(o *ui.Options) { o.StartMode = "invalid" }},
		{"zero width", func(o *ui.Options) { o.PreviewWidth = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := &switchDriver{}
			options := ui.DefaultOptions()
			test.change(&options)
			opened := false
			service := Service{Driver: driver, Options: options,
				RunUI: func(context.Context, list.Snapshot, ui.Options, ui.Runtime) (ui.Result, error) {
					opened = true
					return ui.Result{}, nil
				},
			}

			err := service.Run(t.Context())

			require.Error(t, err)
			require.Zero(t, driver.loads)
			require.False(t, opened)
		})
	}
}

func TestServiceUsesConfiguredStartMode(t *testing.T) {
	for _, test := range []struct {
		configured ui.StartMode
		want       ui.StartMode
	}{
		{configured: ui.StartModeNormal, want: ui.StartModeNormal},
		{configured: ui.StartModeFilter, want: ui.StartModeFilter},
	} {
		var got ui.StartMode
		options := ui.DefaultOptions()
		options.StartMode = test.configured
		service := Service{Options: options,
			Driver: &switchDriver{},
			RunUI: func(_ context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
				require.Equal(t, ui.DefaultPreviewWidth, options.PreviewWidth)
				require.Nil(t, runtime.Status.Read)
				got = options.StartMode
				return ui.Result{}, nil
			},
		}
		require.NoError(t, service.Run(context.Background()))
		require.Equal(t, test.want, got)
	}
}

func TestUIExitCancelsAndJoinsBusyEffect(t *testing.T) {
	driver := &blockingDriver{started: make(chan struct{}), stopped: make(chan struct{})}
	dispatchDone := make(chan error, 1)
	service := Service{Options: ui.DefaultOptions(),
		Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
			go func() {
				_, err := runtime.Dispatch(ui.ActionRequest{ActionID: ui.ActionCreateSession})
				dispatchDone <- err
			}()
			awaitChannel(t, driver.started)
			return ui.Result{}, nil
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

func (d *navigationDriver) Load(context.Context, ui.ViewMode) (list.Snapshot, error) {
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
	service := Service{Options: ui.DefaultOptions(),
		Driver: &switchDriver{},
		RunUI: func(ctx context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
			close(started)
			<-ctx.Done()
			return ui.Result{}, tea.ErrProgramKilled
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
	service := Service{Options: ui.DefaultOptions(),
		Driver: driver,
		RunUI: func(context.Context, list.Snapshot, ui.Options, ui.Runtime) (ui.Result, error) {
			return ui.Result{Navigation: list.ItemID{ID: "dev"}}, nil
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
	service := Service{Options: ui.DefaultOptions(),
		Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
			defer func() { driver.uiReturned = true }()
			result, err := runtime.Dispatch(ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: list.ItemID{ID: "dev"}})
			return ui.Result{Navigation: result.Navigation}, err
		},
	}

	require.NoError(t, service.Run(context.Background()))
	assert.Equal(t, 1, driver.loads)
	assert.Equal(t, []list.ItemID{{ID: "dev"}}, driver.navigations)
}
