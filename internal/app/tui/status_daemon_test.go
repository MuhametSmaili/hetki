package tui

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestCachedLocationsRebindLinkedRowsAndRespawns(t *testing.T) {
	state := linkedPaneState()
	server := observe.Server{PID: 123, Birth: "fixture", Started: 1}
	b := &projectionStatusBackend{stubBackend: stubBackend{state: state}, server: server}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	adapter.projection = projectionTree
	_, err := adapter.Load(t.Context())
	require.NoError(t, err)
	ref := status.PaneRef{Server: server.Identity(), ID: "%1", Generation: "respawned"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{ref: {State: status.Blocked, ValidUntil: time.Now().Add(time.Minute)}})
	require.NoError(t, err)
	view := daemon.View{Values: values, Locations: []daemon.Location{
		{Session: "$1", Window: "@1", Pane: "%1"},
		{Session: "$2", Window: "@1", Pane: "%1"},
	}}

	bindings := adapter.bindStatusView(view)

	require.Equal(t, ui.StatusBindings{
		windowItemID("$1", "@1"): {ref}, windowItemID("$2", "@1"): {ref},
	}, bindings)
	var update ui.StatusUpdate
	adapter.bindStatusUpdate(view, &update)
	require.Equal(t, ref, update.Windows[windowItemID("$1", "@1")][0].Ref)
	require.Equal(t, ref, update.Windows[windowItemID("$2", "@1")][0].Ref)
	unlinked := view
	unlinked.Locations = view.Locations[:1]

	adapter.bindStatusUpdate(unlinked, &update)

	require.Equal(t, ref, update.Windows[windowItemID("$1", "@1")][0].Ref)
	require.Empty(t, update.Windows[windowItemID("$2", "@1")][0].Ref, "pane identity alone must not bind a removed window link")

	bindings[windowItemID("$1", "@1")][0] = status.PaneRef{}
	delete(bindings, windowItemID("$2", "@1"))
	require.Equal(t, ref, adapter.bindStatusView(view)[windowItemID("$1", "@1")][0])
	require.Len(t, adapter.bindStatusView(view), 2, "bindings own their map and pane slices")

	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})
	require.NoError(t, err)
	flat := adapter.bindStatusView(view)
	require.Equal(t, ui.StatusBindings{
		destinationItemID("$1", "@1", "%1"): {ref}, destinationItemID("$2", "@1", "%1"): {ref},
	}, flat)
	view.Locations = []daemon.Location{{Session: "$1", Window: "@99", Pane: "%1"}}
	require.Empty(t, adapter.bindStatusView(view), "moved pane must not bind its old window occurrence")
	pane := b.state.Sessions[0].Windows[0].Panes[0]
	for i := range b.state.Sessions {
		b.state.Sessions[i].Windows[0].Panes = b.state.Sessions[i].Windows[0].Panes[1:]
	}
	b.state.Sessions[0].Windows = append(b.state.Sessions[0].Windows, backend.Window{
		ID: "@99", Name: "moved", Panes: []backend.Pane{pane},
	})
	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionRefresh})
	require.NoError(t, err)
	require.Equal(t, ui.StatusBindings{destinationItemID("$1", "@99", "%1"): {ref}}, adapter.bindStatusView(view))

	adapter.statusServer.Birth = "replacement server"
	require.Empty(t, adapter.bindStatusView(view), "reused backend IDs must not bind another server's evidence")
}

type openingStatusDriver struct {
	switchDriver
	err error
}

func (d *openingStatusDriver) OpenStatus(context.Context) (*statusDelivery, error) {
	return nil, d.err
}

func TestStatusAvailabilityKeepsPickerUsable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		driver Driver
		notice string
	}{
		{"absent", &switchDriver{}, ""},
		{"opening fails", &openingStatusDriver{err: errors.New("invalid profile fixture.yaml")}, "status unavailable: invalid profile fixture.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := Service{Driver: tc.driver, RunUI: func(_ context.Context, snapshot list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
				require.Equal(t, tc.notice, snapshot.Notice)
				require.Nil(t, options.Read)
				return list.ItemID{}, nil
			}}

			err := service.Run(t.Context())

			require.NoError(t, err)
		})
	}
}

type replacedServerBackend struct {
	stubBackend
}

func (b *replacedServerBackend) QueryStatusState(context.Context) (backend.StateResult, observe.Server, error) {
	return backend.StateResult{}, observe.Server{}, observe.ErrServerChanged
}

func TestOpeningStatusDefersDaemonWorkUntilRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		adapter := &LiveAdapter{statusServer: observe.Server{PID: -1}}

		reader, err := adapter.OpenStatus(t.Context())

		require.NoError(t, err, "opening must not validate or launch the daemon")
		require.NotNil(t, reader)
		update := reader.options().Read(t.Context(), status.Snapshot{})
		require.Error(t, update.Err, "the asynchronous read validates the descriptor")
		acceptStatusUpdate(update)

		time.Sleep(time.Hour) // Virtual time exceeds any retry suppression.
		later := reader.options().Read(t.Context(), update.Values)

		require.ErrorIs(t, later.Err, errors.Unwrap(update.Err), "uncertain native identity ends this reader")
	})
}

func TestDaemonReaderWaitHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader := newDaemonReader(t.Context(), observe.Server{})
		reader.start.Do(func() {}) // Keep the subscription goroutine from starting.
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, _, err := reader.readView(ctx, 0)
			done <- err
		}()
		synctest.Wait()

		cancel()

		require.ErrorIs(t, awaitChannel(t, done), context.Canceled)
	})
}

func TestServerReplacementDuringLoadDisablesStatusBinding(t *testing.T) {
	b := &replacedServerBackend{stubBackend: stubBackend{state: liveState()}}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)

	_, loadErr := adapter.Load(t.Context())
	reader, statusErr := adapter.OpenStatus(t.Context())

	require.NoError(t, loadErr, "browsing does not depend on status startup")
	require.Nil(t, reader)
	require.ErrorIs(t, statusErr, observe.ErrServerChanged)
}

type scriptedSubscription struct {
	frames []error
	closed chan struct{}
	once   sync.Once
}

func (s *scriptedSubscription) Next() (daemon.View, error) {
	if len(s.frames) > 0 {
		err := s.frames[0]
		s.frames = s.frames[1:]
		return daemon.View{}, err
	}
	<-s.closed
	return daemon.View{}, io.EOF
}

func (s *scriptedSubscription) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func TestDaemonReaderBacksOffAndResetsAfterAFrame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		reader := newDaemonReader(ctx, observe.Server{})
		start := time.Now()
		var attempts []time.Duration
		failure := errors.New("daemon unavailable")
		reader.open = func(context.Context) (subscription, error) {
			attempts = append(attempts, time.Since(start))
			if len(attempts) == 4 {
				// One frame, then the connection breaks.
				return &scriptedSubscription{frames: []error{nil, io.ErrUnexpectedEOF}, closed: make(chan struct{})}, nil
			}
			return nil, failure
		}

		_, _, err := reader.readView(ctx, 0)
		require.ErrorIs(t, err, failure)
		time.Sleep(20 * time.Second) // Virtual time.
		cancel()
		reader.wait()

		require.Equal(t, []time.Duration{0, 2 * time.Second, 6 * time.Second, 11 * time.Second, 13 * time.Second, 17 * time.Second}, attempts)
	})
}

func TestDaemonReaderKeepsSubscriptionAfterUnavailableFrame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reader := newDaemonReader(ctx, observe.Server{})
		opened := 0
		reader.open = func(context.Context) (subscription, error) {
			opened++
			return &scriptedSubscription{frames: []error{daemon.ErrUnavailable, nil}, closed: make(chan struct{})}, nil
		}

		_, _, _ = reader.readView(ctx, 0)
		synctest.Wait()
		_, sequence, err := reader.readView(ctx, 0)

		require.NoError(t, err, "a valid frame follows the unavailable one")
		require.EqualValues(t, 2, sequence, "both frames arrived on one subscription")
		require.Equal(t, 1, opened)
		cancel()
		reader.wait()
	})
}
