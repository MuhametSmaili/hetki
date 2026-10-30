package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stayOpenDriver struct {
	switchDriver
	executeErr  error
	navigateErr error
	requests    []ui.ActionRequest
}

func (d *stayOpenDriver) Execute(_ context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
	d.requests = append(d.requests, request)
	target := request.ItemID
	if request.ActionID == ui.ActionLastSession {
		target = sessionItemID("$2")
	}
	return ui.ActionResult{Navigation: target}, d.executeErr
}

func (d *stayOpenDriver) Navigate(_ context.Context, target list.ItemID) error {
	if d.uiReturned {
		return assert.AnError
	}
	d.navigations = append(d.navigations, target)
	return d.navigateErr
}

func TestStayOpenSwitchesRepeatedlyDuringOneUIRun(t *testing.T) {
	driver := &stayOpenDriver{}
	runs := 0
	service := Service{Options: ui.DefaultOptions(), StayOpen: true, Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, _ ui.Options, runtime ui.Runtime) (ui.Result, error) {
			defer func() { driver.uiReturned = true }()
			runs++
			for i, tt := range []struct {
				name    string
				request ui.ActionRequest
				target  list.ItemID
			}{
				{name: "open selected pane", request: ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: windowItemID("$1", "@1"), PaneID: "%2"}, target: windowItemID("$1", "@1")},
				{name: "previous session", request: ui.ActionRequest{ActionID: ui.ActionLastSession}, target: sessionItemID("$2")},
			} {
				t.Run(tt.name, func(t *testing.T) {
					result, err := runtime.Dispatch(tt.request)

					require.NoError(t, err)
					require.True(t, result.Navigation.IsZero(), "navigation must not request UI exit")
					require.Equal(t, tt.request, driver.requests[i])
					require.Len(t, driver.navigations, i+1)
					require.Equal(t, tt.target, driver.navigations[i])
				})
			}
			return ui.Result{}, nil
		},
	}

	err := service.Run(t.Context())

	require.NoError(t, err)
	require.Equal(t, 1, runs)
	require.Equal(t, 1, driver.loads)
	require.Len(t, driver.navigations, 2, "quitting must not repeat navigation")
}

func historyState() backend.StateResult {
	state := liveState()
	state.Sessions = append(state.Sessions,
		backend.Session{ID: "$2", Name: "previous", Last: true, Windows: []backend.Window{
			{ID: "@2", Active: true, Panes: []backend.Pane{{ID: "%2", Path: "/previous", Active: true}}},
		}},
		backend.Session{ID: "$3", Name: "visited", Windows: []backend.Window{
			{ID: "@3", Active: true, Panes: []backend.Pane{{ID: "%3", Path: "/visited", Active: true}}},
		}},
	)
	return state
}

func TestStayOpenUsesCurrentPreviousSession(t *testing.T) {
	for _, tt := range []struct {
		name            string
		view            ui.ViewMode
		initialPrevious bool
	}{
		{name: "flat replaces startup history", view: ui.ViewFlat, initialPrevious: true},
		{name: "tree replaces startup history", view: ui.ViewTree, initialPrevious: true},
		{name: "flat discovers new history", view: ui.ViewFlat},
		{name: "tree discovers new history", view: ui.ViewTree},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubBackend{state: historyState()}
			stub.state.Sessions[1].Last = tt.initialPrevious
			options := ui.DefaultOptions()
			options.Initial.View = tt.view
			service := Service{
				Driver:   testAdapter(stub),
				Options:  options,
				StayOpen: true,
				RunUI: func(_ context.Context, snapshot list.Snapshot, _ ui.Options, runtime ui.Runtime) (ui.Result, error) {
					selected := destinationItemID("$3", "@3", "%3")
					if tt.view == ui.ViewTree {
						selected = windowItemID("$3", "@3")
					}
					_, err := runtime.Dispatch(ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: selected})
					require.NoError(t, err)

					stub.state.Sessions[1].Last = false
					stub.state.Sessions[2].Last = true

					result, err := runtime.Dispatch(ui.ActionRequest{ActionID: ui.ActionLastSession})

					require.NoError(t, err)
					require.Len(t, stub.switchCalls, 2)
					require.Equal(t, "$3", stub.switchCalls[1].SessionID)
					require.True(t, result.Navigation.IsZero())
					require.NotNil(t, result.Snapshot)
					require.Equal(t, snapshot.ActiveItemID, result.Snapshot.ActiveItemID)
					requireHistoryMarker(t, *result.Snapshot, selected)
					return ui.Result{}, nil
				},
			}

			err := service.Run(t.Context())

			require.NoError(t, err)
			require.Equal(t, 3, stub.queryCalls, "each open should query once without reloading the picker")
		})
	}
}

func requireHistoryMarker(t *testing.T, snapshot list.Snapshot, want list.ItemID) {
	t.Helper()
	items, err := list.New(snapshot)
	require.NoError(t, err)
	var marked []list.ItemID
	for _, row := range items.Rows() {
		if strings.HasSuffix(row.Item.Primary, " ↶") {
			marked = append(marked, row.Item.ID)
		}
	}
	require.Equal(t, []list.ItemID{want}, marked)
}

func TestStayOpenReturnsSwitchFailuresToUIAndAllowsRetry(t *testing.T) {
	for _, tt := range []struct {
		name        string
		executeErr  error
		navigateErr error
		attempts    int
	}{
		{name: "preparation failed", executeErr: assert.AnError},
		{name: "navigation failed", navigateErr: assert.AnError, attempts: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			driver := &stayOpenDriver{executeErr: tt.executeErr, navigateErr: tt.navigateErr}
			service := Service{Options: ui.DefaultOptions(), StayOpen: true, Driver: driver,
				RunUI: func(_ context.Context, _ list.Snapshot, _ ui.Options, runtime ui.Runtime) (ui.Result, error) {
					defer func() { driver.uiReturned = true }()
					request := ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: sessionItemID("$1")}

					_, err := runtime.Dispatch(request)

					require.ErrorIs(t, err, assert.AnError)
					require.Len(t, driver.navigations, tt.attempts)
					driver.executeErr, driver.navigateErr = nil, nil

					result, err := runtime.Dispatch(request)

					require.NoError(t, err)
					require.True(t, result.Navigation.IsZero())
					return ui.Result{}, nil
				},
			}

			err := service.Run(t.Context())

			require.NoError(t, err)
			require.Len(t, driver.navigations, tt.attempts+1)
		})
	}
}

type blockingStayOpenDriver struct {
	navigationDriver
}

func (d *blockingStayOpenDriver) Execute(_ context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
	return ui.ActionResult{Navigation: request.ItemID}, nil
}

func TestStayOpenQuitCancelsAndJoinsNavigation(t *testing.T) {
	driver := &blockingStayOpenDriver{navigationDriver{started: make(chan struct{})}}
	dispatchDone := make(chan error, 1)
	var dispatch ui.DispatchFunc
	service := Service{Options: ui.DefaultOptions(), StayOpen: true, Driver: driver,
		RunUI: func(_ context.Context, _ list.Snapshot, _ ui.Options, runtime ui.Runtime) (ui.Result, error) {
			dispatch = runtime.Dispatch
			go func() {
				_, err := dispatch(ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: sessionItemID("$1")})
				dispatchDone <- err
			}()
			awaitChannel(t, driver.started)
			return ui.Result{}, nil
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := service.Run(ctx)

	require.NoError(t, err)
	require.ErrorIs(t, awaitChannel(t, dispatchDone), context.Canceled)
	_, err = dispatch(ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: sessionItemID("$1")})
	require.ErrorIs(t, err, context.Canceled, "late effects must not run after shutdown")
}
