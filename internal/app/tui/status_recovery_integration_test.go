//go:build integration

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "__status-daemon" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := statusobserver.ServeDaemon(ctx, os.Args[2], os.Args[3])
		cancel()
		stop()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type steadyStatusSource struct {
	unwatchedStatusSource
	after chan uint64
}

func (s *steadyStatusSource) WatchView(ctx context.Context, after uint64) (daemon.View, error) {
	select {
	case s.after <- after:
	case <-ctx.Done():
		return daemon.View{}, ctx.Err()
	}
	if after != 0 {
		<-ctx.Done()
	}
	return s.view, nil // A late successful result must still respect caller cancellation.
}

func TestNativePickerCanceledWatchRequestsFreshView(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	source := &steadyStatusSource{after: make(chan uint64, 3)}
	source.view = blockedPickerView(t, server.Identity())
	source.view.Cursor.Revision = 1
	ref := firstPaneRef(t, source.view.Values)
	servePickerSource(t, client, server.Identity(), source)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		initial := options.Read(ctx, status.Snapshot{})
		require.NoError(t, initial.Err)
		require.Zero(t, awaitChannel(t, source.after))
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
		require.EqualValues(t, 1, awaitChannel(t, source.after))

		cancel()
		canceled := awaitChannel(t, finished)

		require.NoError(t, canceled.Err)
		require.Equal(t, status.Unknown, canceled.Values.Lookup(ref).State)
		acceptStatusUpdate(canceled)
		nextCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		next := options.Read(nextCtx, canceled.Values)
		require.NoError(t, next.Err, "canceling a healthy watch must not suppress the next read")
		require.Zero(t, awaitChannel(t, source.after), "an abandoned watch must request a full current view")
		require.Equal(t, status.Blocked, next.Values.Lookup(ref).State)
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}

func TestNativePickerDiscardedDeliveryRequestsFreshView(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	source := &steadyStatusSource{after: make(chan uint64, 2)}
	source.view = blockedPickerView(t, server.Identity())
	source.view.Cursor.Revision = 1
	ref := firstPaneRef(t, source.view.Values)
	servePickerSource(t, client, server.Identity(), source)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	_, err = adapter.Load(t.Context())
	require.NoError(t, err)
	delivery, err := adapter.OpenStatus(t.Context())
	require.NoError(t, err)
	read := delivery.readView
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	delivery.readView = func(ctx context.Context, after daemon.Cursor) (daemon.View, error) {
		view, err := read(ctx, after)
		cancel()
		return view, err
	}

	discarded := delivery.options().Read(ctx, status.Snapshot{})

	require.NoError(t, discarded.Err)
	require.Zero(t, discarded.Values.Len(), "cancellation during handoff must discard successful evidence")
	require.Zero(t, awaitChannel(t, source.after))
	acceptStatusUpdate(discarded)
	nextCtx, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	next := delivery.options().Read(nextCtx, discarded.Values)
	require.NoError(t, next.Err)
	require.Equal(t, status.Blocked, next.Values.Lookup(ref).State)
	require.Zero(t, awaitChannel(t, source.after), "discarded delivery must not advance the next request cursor")
}

type recoveringStatusSource struct {
	unwatchedStatusSource
	unavailable  atomic.Bool
	firstStarted chan struct{}
}

func (s *recoveringStatusSource) ReadView(ctx context.Context) (daemon.View, error) {
	if s.reads.Add(1) == 1 {
		close(s.firstStarted)
		<-ctx.Done()
		return s.view, nil
	}
	if s.unavailable.Load() {
		return daemon.View{}, daemon.ErrUnavailable
	}
	return s.view, nil
}

func TestNativePickerCapsRetriesUntilACompleteView(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	source := &recoveringStatusSource{firstStarted: make(chan struct{})}
	source.view = blockedPickerView(t, server.Identity())
	source.unavailable.Store(true)
	servePickerSource(t, client, server.Identity(), source)

	synctest.Test(t, func(t *testing.T) {
		adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
		service := Service{Driver: adapter}
		service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
			readCtx, cancel := context.WithCancel(ctx)
			finished := make(chan ui.StatusUpdate, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				finished <- options.Read(readCtx, status.Snapshot{})
			}()
			defer func() {
				cancel()
				awaitChannel(t, done)
			}()
			awaitChannel(t, source.firstStarted)

			cancel()
			canceled := awaitChannel(t, finished)

			require.NoError(t, canceled.Err)
			require.Zero(t, canceled.Values.Len(), "canceled startup must discard late evidence")
			acceptStatusUpdate(canceled)
			failed := options.Read(ctx, status.Snapshot{})
			require.ErrorIs(t, failed.Err, context.Canceled)
			require.EqualValues(t, 1, source.reads.Load(), "canceled startup must suppress immediate retries")
			acceptStatusUpdate(failed)
			for _, delay := range []time.Duration{2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second} {
				reads := source.reads.Load()
				cause := errors.Unwrap(failed.Err)
				time.Sleep(delay - time.Nanosecond) // Virtual time, not a real sleep.
				early := options.Read(ctx, status.Snapshot{})
				require.ErrorIs(t, early.Err, cause)
				require.Equal(t, reads, source.reads.Load(), "suppression must avoid I/O")
				acceptStatusUpdate(early)

				time.Sleep(time.Nanosecond)
				failed = options.Read(ctx, status.Snapshot{})

				require.ErrorIs(t, failed.Err, daemon.ErrUnavailable)
				require.Equal(t, reads+1, source.reads.Load(), "successful negotiation alone must not reset backoff")
				acceptStatusUpdate(failed)
			}
			source.unavailable.Store(false)
			time.Sleep(5 * time.Second)
			recovered := options.Read(ctx, status.Snapshot{})
			require.NoError(t, recovered.Err)
			acceptStatusUpdate(recovered)
			source.unavailable.Store(true)
			failed = options.Read(ctx, recovered.Values)
			require.ErrorIs(t, failed.Err, daemon.ErrUnavailable)
			acceptStatusUpdate(failed)
			reads := source.reads.Load()

			time.Sleep(2*time.Second - time.Nanosecond)
			early := options.Read(ctx, recovered.Values)
			require.ErrorIs(t, early.Err, daemon.ErrUnavailable)
			require.Equal(t, reads, source.reads.Load(), "recovery must preserve the minimum retry delay")
			acceptStatusUpdate(early)

			time.Sleep(time.Nanosecond)
			failed = options.Read(ctx, recovered.Values)

			require.ErrorIs(t, failed.Err, daemon.ErrUnavailable)
			require.Equal(t, reads+1, source.reads.Load(), "a complete view resets the retry delay")
			return list.ItemID{}, nil
		}

		require.NoError(t, service.Run(t.Context()))
	})
}

func TestNativePickerRecoversAfterDaemonLoss(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		initial := awaitPickerValue(t, ctx, options, status.Value{State: status.Idle}, nil)
		before, err := client.Ping(ctx)
		require.NoError(t, err)
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		require.NoError(t, client.Stop(stopCtx))
		require.NoError(t, testenv.WaitUnlocked(stopCtx, strings.TrimSuffix(client.LogPath(), ".log")+".sock.lock"))
		cancel()

		failed := options.Read(ctx, initial.Values)

		require.Error(t, failed.Err, "loss must be delivered before attempting recovery")
		require.Nil(t, failed.Bindings)
		pane := firstPaneRef(t, initial.Values)
		require.Equal(t, status.Unknown, failed.Values.Lookup(pane).State)
		acceptStatusUpdate(failed)
		recovered := awaitPickerValue(t, ctx, options, status.Value{State: status.Idle}, nil)
		after, err := client.Ping(ctx)
		require.NoError(t, err)
		require.NotEqual(t, before.Epoch, after.Epoch)
		require.True(t, recovered.Watching)
		require.Len(t, recovered.Bindings, 2)
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}

func TestNativePickerRecoversAfterProfileCorrection(t *testing.T) {
	b, _, _ := pickerDaemonFixture(t)
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "hetki", "status.d", "cat.yaml")
	profile, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("invalid: profile\n"), 0600))
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		failed := options.Read(ctx, status.Snapshot{})
		require.Error(t, failed.Err)
		acceptStatusUpdate(failed)

		require.NoError(t, os.WriteFile(path, profile, 0600))
		recovered := awaitPickerValue(t, ctx, options, status.Value{State: status.Idle}, nil)

		require.True(t, recovered.Watching)
		require.Len(t, recovered.Bindings, 2, "the same reader must recover both linked occurrences")
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}
