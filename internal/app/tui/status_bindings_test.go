package tui

import (
	"context"
	"errors"
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

type viewWatcher struct {
	failingViewReader
	read func(context.Context) (daemon.View, error)
}

func (r viewWatcher) WatchView(ctx context.Context) (daemon.View, error) {
	return r.read(ctx)
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
	return adapter, daemon.View{Values: values, Locations: []daemon.Location{{Session: "$1", Window: "@1", Pane: "%1"}}, Cursor: daemon.Cursor{Revision: 1}}
}

func TestServiceSuppliesBindingsOnProjectionReplacement(t *testing.T) {
	adapter, view := statusDeliveryFixture(t)
	now := time.Now()
	reads := 0
	reader := viewWatcher{read: func(context.Context) (daemon.View, error) {
		reads++
		return view, nil
	}}
	service := Service{Driver: adapter, StatusReader: reader}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		require.Empty(t, options.Bindings, "ordinary backend rows have no status references")
		initial := options.Read(ctx, status.Snapshot{})
		require.NoError(t, initial.Err)
		require.Equal(t, status.Blocked, initial.Bindings.StateAt(windowItemID("$1", "@1"), initial.Values, now))
		acceptStatusUpdate(initial)

		for _, id := range []list.ItemID{destinationItemID("$1", "@1", "%1"), windowItemID("$1", "@1")} {
			result, err := dispatch(ui.ActionRequest{ActionID: ui.ActionToggleProjection})
			require.NoError(t, err)
			require.NotNil(t, result.Snapshot)
			require.NotNil(t, result.Status)
			require.Equal(t, status.Blocked, result.Status.Bindings.StateAt(id, result.Status.Values, now))
			acceptStatusUpdate(*result.Status)
		}
		require.Equal(t, 1, reads, "replacement must not depend on another transport read")
		return list.ItemID{}, nil
	}

	err := service.Run(t.Context())

	require.NoError(t, err)
}

type statusChangeWatcher struct {
	failingViewReader
	view             daemon.View
	err              error
	started, release chan struct{}
	reads            int
}

func (r *statusChangeWatcher) WatchView(ctx context.Context) (daemon.View, error) {
	r.reads++
	if r.reads == 1 {
		return r.view, nil
	}
	if r.reads != 2 {
		return daemon.View{}, errors.New("unexpected transport read instead of replay")
	}
	close(r.started)
	select {
	case <-r.release:
		view := r.view
		view.Values = view.Values.Invalidate()
		return view, r.err
	case <-ctx.Done():
		return daemon.View{}, ctx.Err()
	}
}

func TestServiceReplaysStatusReceivedDuringProjectionReplacement(t *testing.T) {
	failure := errors.New("daemon gone")
	for _, tc := range []struct {
		name   string
		before bool
		err    error
	}{
		{"read completes before replacement", true, nil},
		{"read completes after replacement", false, nil},
		{"read fails before replacement", true, failure},
		{"read fails after replacement", false, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, view := statusDeliveryFixture(t)
			reader := &statusChangeWatcher{view: view, err: tc.err, started: make(chan struct{}), release: make(chan struct{})}
			service := Service{Driver: adapter, StatusReader: reader}
			service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
				initial := options.Read(ctx, status.Snapshot{})
				acceptStatusUpdate(initial)
				readCtx, cancel := context.WithCancel(ctx)
				finished := make(chan ui.StatusUpdate, 1)
				done := make(chan struct{})
				go func() {
					defer close(done)
					finished <- options.Read(readCtx, initial.Values)
				}()
				defer func() {
					cancel()
					awaitChannel(t, done)
				}()
				awaitChannel(t, reader.started)
				var received ui.StatusUpdate
				if tc.before {
					close(reader.release)
					received = awaitChannel(t, finished)
				}

				result, err := dispatch(ui.ActionRequest{ActionID: ui.ActionToggleProjection})
				require.NoError(t, err)
				applied := *result.Status
				if tc.before {
					acceptStatusUpdate(applied)
				} else {
					close(reader.release)
					received = awaitChannel(t, finished)
					acceptStatusUpdate(applied)
					applied = options.Read(ctx, applied.Values)
				}

				require.ErrorIs(t, received.Err, tc.err)
				require.Equal(t, tc.err != nil, received.Bindings == nil, "failed reads preserve binding ownership")
				require.Equal(t, status.Unknown, initial.Bindings.StateAt(windowItemID("$1", "@1"), received.Values, time.Now()))
				require.ErrorIs(t, applied.Err, tc.err)
				bindings := applied.Bindings
				if bindings == nil {
					bindings = result.Status.Bindings
				}
				require.Equal(t, status.Unknown, bindings.StateAt(destinationItemID("$1", "@1", "%1"), applied.Values, time.Now()))
				require.Equal(t, 2, reader.reads, "dropped delivery must not wait for another daemon change")
				return list.ItemID{}, nil
			}

			require.NoError(t, service.Run(t.Context()))
		})
	}
}

func acceptStatusUpdate(update ui.StatusUpdate) {
	if update.Accept != nil {
		update.Accept()
	}
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
	var read func(context.Context, status.Snapshot) ui.StatusUpdate
	service := Service{Driver: &switchDriver{}, StatusReader: statusReaderFunc(func(ctx context.Context) (status.Snapshot, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return status.Snapshot{}, ctx.Err()
	}), RunUI: func(_ context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		read = options.Read
		go func() { done <- read(context.Background(), previous).Values }()
		awaitChannel(t, started)
		return list.ItemID{}, nil
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
	require.Equal(t, status.Unknown, late.Values.At(pane, now))
}
