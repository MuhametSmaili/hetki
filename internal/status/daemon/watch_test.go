package daemon

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

type watchSource struct {
	core    *service.Service
	waiting chan struct{}
}

func (s *watchSource) ReadStatus(ctx context.Context) (status.Snapshot, error) {
	return s.core.ReadStatus(ctx)
}

func (s *watchSource) Close() error { return s.core.Close() }

func (s *watchSource) ReadView(ctx context.Context) (View, error) {
	revision, _ := s.core.Changes()
	values, err := s.core.ReadStatus(ctx)
	view := View{Values: values, Cursor: Cursor{Revision: revision}}
	for ref := range values.All() {
		view.Locations = append(view.Locations, Location{Session: "$1", Window: "@1", Pane: ref.ID})
	}
	return view, err
}

func (s *watchSource) WatchView(ctx context.Context, after uint64) (View, error) {
	for {
		revision, changes := s.core.Changes()
		if revision != after {
			return s.ReadView(ctx)
		}
		select {
		case s.waiting <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return View{}, ctx.Err()
		case <-changes:
		}
	}
}

func watchFixture(t *testing.T) (*Client, *watchSource, service.Target) {
	t.Helper()
	client := fixtureClient(t, "watch")
	core, err := service.New(client.server)
	require.NoError(t, err)
	pane := service.Pane{
		Ref:   status.PaneRef{Server: client.server, ID: "%1", Generation: "birth"},
		Scope: service.Scope{Root: "root", Foreground: "foreground", Connection: "connection"},
	}
	require.NoError(t, core.UpdateInventory(service.Inventory{
		Revision:   1,
		ValidUntil: time.Now().Add(time.Minute),
		Panes:      []service.Pane{pane},
	}))
	target, ok := core.Target(pane.Ref, service.Protocol)
	require.True(t, ok)
	require.NoError(t, core.Apply(target, 1, status.Value{State: status.Idle}))
	source := &watchSource{core: core, waiting: make(chan struct{}, 32)}
	startFixture(t, client, liveBinding(client.server), func(context.Context, func(error)) (Source, error) { return source, nil })
	return client, source, target
}

func TestWatchDeliversEvidenceWithCursorAndLocations(t *testing.T) {
	client, source, target := watchFixture(t)
	info, err := client.Ping(t.Context())
	require.NoError(t, err)
	watch, err := client.SupportsWatch(t.Context())
	require.NoError(t, err)
	require.True(t, watch)
	initial, err := client.WatchView(t.Context(), Cursor{})
	require.NoError(t, err)
	updates := make(chan View, 1)
	errors := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	go func() {
		view, err := client.WatchView(ctx, initial.Cursor)
		updates <- view
		errors <- err
	}()
	receive(t, source.waiting)

	require.NoError(t, source.core.Apply(target, 2, status.Value{State: status.Blocked}))
	updated := receive(t, updates)

	require.NoError(t, receive(t, errors))
	require.Equal(t, status.Blocked, updated.Values.At(target.Pane.Ref, time.Now()))
	require.Greater(t, updated.Cursor.Revision, initial.Cursor.Revision)
	require.Equal(t, info.Epoch, updated.Cursor.Epoch)
	require.Equal(t, initial.Locations, updated.Locations)
}

func TestWatchCoalescesUpdatesBetweenReads(t *testing.T) {
	client, source, target := watchFixture(t)
	initial, err := client.WatchView(t.Context(), Cursor{})
	require.NoError(t, err)

	require.NoError(t, source.core.Apply(target, 2, status.Value{State: status.Working}))
	require.NoError(t, source.core.Apply(target, 3, status.Value{State: status.Idle}))
	latest, err := client.WatchView(t.Context(), initial.Cursor)

	require.NoError(t, err)
	require.Greater(t, latest.Cursor.Revision, initial.Cursor.Revision)
	require.Equal(t, status.Idle, latest.Values.At(target.Pane.Ref, time.Now()))
}

func TestWatchReturnsCurrentViewForForeignEpoch(t *testing.T) {
	client, _, _ := watchFixture(t)
	initial, err := client.WatchView(t.Context(), Cursor{})
	require.NoError(t, err)
	foreign := initial.Cursor
	foreign.Epoch = "previous-daemon"

	rebased, err := client.WatchView(t.Context(), foreign)

	require.NoError(t, err)
	require.Equal(t, initial.Cursor, rebased.Cursor)
}

func TestWatchCapacityReservesControlRequestsAndCancellationReleasesSlots(t *testing.T) {
	client, source, _ := watchFixture(t)
	initial, err := client.WatchView(t.Context(), Cursor{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, maxWatchers)
	for range maxWatchers {
		go func() {
			_, err := client.WatchView(ctx, initial.Cursor)
			finished <- err
		}()
		receive(t, source.waiting)
	}

	_, overflow := client.WatchView(t.Context(), initial.Cursor)
	_, pingErr := client.Ping(t.Context())
	cancel()

	require.ErrorIs(t, overflow, ErrUnavailable)
	require.NoError(t, pingErr)
	for range maxWatchers {
		require.ErrorIs(t, receive(t, finished), context.Canceled)
	}
	require.Eventually(t, func() bool {
		_, err := client.WatchView(t.Context(), Cursor{})
		return err == nil
	}, time.Second, time.Millisecond)
}

func TestWatchDeadlineRenewsSnapshotWithoutReportingFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		core, err := service.New("server")
		require.NoError(t, err)
		defer core.Close()
		source := &watchSource{core: core, waiting: make(chan struct{}, 1)}
		d := daemon{
			source:   source,
			binding:  Binding{Server: "server"},
			info:     Info{Epoch: "epoch"},
			watchers: make(chan struct{}, maxWatchers),
		}
		revision, _ := core.Changes()
		var reply reply
		start := time.Now()

		err = d.readWatchedView(t.Context(), &Cursor{Epoch: "epoch", Revision: revision}, &reply)

		require.NoError(t, err)
		require.Equal(t, watchWait, time.Since(start))
		require.Equal(t, revision, reply.Revision)
	})
}

func TestWatchShutdownInterruptsOutstandingReader(t *testing.T) {
	client, source, _ := watchFixture(t)
	initial, err := client.WatchView(t.Context(), Cursor{})
	require.NoError(t, err)
	finished := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	go func() {
		_, err := client.WatchView(ctx, initial.Cursor)
		finished <- err
	}()
	receive(t, source.waiting)

	err = client.Stop(t.Context())

	require.NoError(t, err)
	require.Error(t, receive(t, finished))
}

func TestWatchCapabilityProbeAcceptsOlderDaemon(t *testing.T) {
	client := fixtureClient(t, "older-daemon")
	listener, cleanup, err := listen(client.path)
	require.NoError(t, err)
	defer cleanup()
	requests := make(chan request, 1)
	finished := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			finished <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		var req request
		err = readFrame(conn, maxRequestBytes, &req)
		requests <- req
		finished <- err
	}()

	supported, err := client.SupportsWatch(t.Context())

	require.NoError(t, err)
	require.False(t, supported)
	require.Equal(t, "capabilities", receive(t, requests).Action)
	require.NoError(t, receive(t, finished))
}
