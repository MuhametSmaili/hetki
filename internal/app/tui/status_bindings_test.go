package tui

import (
	"context"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/status"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func linkedPaneState() backend.StateResult {
	state := liveState()
	window := &state.Sessions[0].Windows[0]
	window.Panes[0].StatusRef = status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	window.Panes = append(window.Panes, backend.Pane{
		ID: "%2", Index: 1, Path: "/work",
		StatusRef: status.PaneRef{Server: "server", ID: "%2", Generation: "birth"},
	})
	state.Sessions = append(state.Sessions, backend.Session{ID: "$2", Name: "linked", Windows: []backend.Window{*window}})
	return state
}

func TestStatusBindingsForLinkedWindows(t *testing.T) {
	for _, test := range []struct {
		name       string
		projection projectionKind
	}{
		{"tree", projectionTree},
		{"flat", projectionFlat},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := linkedPaneState()
			panes := state.Sessions[0].Windows[0].Panes
			adapter := testAdapter(&stubBackend{state: state})
			adapter.projection = test.projection
			want := ui.StatusBindings{}
			for _, session := range []string{"$1", "$2"} {
				if test.projection == projectionTree {
					want[windowItemID(session, "@1")] = []status.PaneRef{panes[0].StatusRef, panes[1].StatusRef}
				} else {
					for _, pane := range panes {
						want[destinationItemID(session, "@1", pane.ID)] = []status.PaneRef{pane.StatusRef}
					}
				}
			}

			_, err := adapter.Load(context.Background())
			bindings := adapter.StatusBindings()

			require.NoError(t, err)
			require.Equal(t, want, bindings)
			for id := range bindings {
				item := adapter.index[id]
				require.Equal(t, item.SessionTarget+":@1", item.MutationTarget)
			}
		})
	}
}

func TestStatusBindingsFollowPaneMoves(t *testing.T) {
	stub := &stubBackend{state: linkedPaneState()}
	adapter := loadedAdapter(t, stub)
	panes := stub.state.Sessions[0].Windows[0].Panes
	for i := range stub.state.Sessions {
		stub.state.Sessions[i].Windows[0].Panes = stub.state.Sessions[i].Windows[0].Panes[:1]
	}
	stub.state.Sessions[0].Windows = append(stub.state.Sessions[0].Windows, backend.Window{
		ID: "@2", Index: 2, Panes: []backend.Pane{panes[1]},
	})

	_, err := adapter.Load(context.Background())
	bindings := adapter.StatusBindings()

	require.NoError(t, err)
	require.Equal(t, ui.StatusBindings{
		windowItemID("$1", "@1"): {panes[0].StatusRef},
		windowItemID("$2", "@1"): {panes[0].StatusRef},
		windowItemID("$1", "@2"): {panes[1].StatusRef},
	}, bindings)
}

func TestStatusBindingsOwnTheirPaneRefs(t *testing.T) {
	state := linkedPaneState()
	adapter := loadedAdapter(t, &stubBackend{state: state})
	id := windowItemID("$1", "@1")
	want := state.Sessions[0].Windows[0].Panes[0].StatusRef
	bindings := adapter.StatusBindings()

	bindings[id][0] = status.PaneRef{}
	delete(bindings, windowItemID("$2", "@1"))

	require.Equal(t, want, adapter.StatusBindings()[id][0])
	require.Contains(t, adapter.StatusBindings(), windowItemID("$2", "@1"))
}

func TestStatusBindingsRequireMatchingIncarnation(t *testing.T) {
	for _, test := range []struct {
		name string
		ref  status.PaneRef
	}{
		{"unavailable", status.PaneRef{}},
		{"missing generation", status.PaneRef{Server: "server", ID: "%1"}},
		{"missing server", status.PaneRef{ID: "%1", Generation: "birth"}},
		{"different pane", status.PaneRef{Server: "server", ID: "%2", Generation: "birth"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := liveState()
			state.Sessions[0].Windows[0].Panes[0].StatusRef = test.ref
			adapter := testAdapter(&stubBackend{state: state})

			_, err := adapter.Load(context.Background())
			bindings := adapter.StatusBindings()

			require.NoError(t, err)
			require.Empty(t, bindings)
		})
	}
}

func TestServiceSuppliesBindingsOnProjectionReplacement(t *testing.T) {
	state := liveState()
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	state.Sessions[0].Windows[0].Panes[0].StatusRef = pane
	adapter := testAdapter(&stubBackend{state: state})
	reads := 0
	var initial ui.StatusBindings
	var result ui.ActionResult
	var dispatchErr error
	service := Service{Driver: adapter, StatusReader: statusReaderFunc(func(context.Context) (status.Snapshot, error) {
		reads++
		return status.Snapshot{}, nil
	}), RunUI: func(_ context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (ui.BackendTarget, error) {
		initial = options.Bindings
		result, dispatchErr = dispatch(ui.ActionRequest{ActionID: ui.ActionToggleProjection, ItemID: windowItemID("$1", "@1")})
		return "", nil
	}}

	err := service.Run(context.Background())

	require.NoError(t, err)
	require.NoError(t, dispatchErr)
	require.Equal(t, ui.StatusBindings{windowItemID("$1", "@1"): {pane}}, initial)
	require.Equal(t, ui.StatusBindings{destinationItemID("$1", "@1", "%1"): {pane}}, result.StatusBindings)
	require.Zero(t, reads)
}

func TestServiceCancelsAndJoinsStatusOnExit(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	started, stopped := make(chan struct{}), make(chan struct{})
	done := make(chan status.Snapshot, 1)
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	previous, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Working, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	var read func(context.Context, status.Snapshot) status.Snapshot
	service := Service{Driver: &switchDriver{}, StatusReader: statusReaderFunc(func(ctx context.Context) (status.Snapshot, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return status.Snapshot{}, ctx.Err()
	}), RunUI: func(_ context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (ui.BackendTarget, error) {
		read = options.Read
		go func() { done <- read(context.Background(), previous) }()
		awaitChannel(t, started)
		return "", nil
	}}

	err = service.Run(context.Background())
	late := read(context.Background(), previous)

	require.NoError(t, err)
	select {
	case <-stopped:
	default:
		t.Fatal("status reader outlived UI")
	}
	require.Equal(t, status.Unknown, awaitChannel(t, done).At(pane, now))
	require.Equal(t, status.Unknown, late.At(pane, now))
}
