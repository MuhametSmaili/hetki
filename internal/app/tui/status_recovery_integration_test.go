//go:build integration

package tui

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
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

func TestNativePickerCanceledReadReturnsCurrentView(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	source := newPickerStatusSource(blockedPickerView(t, server.Identity()))
	ref := firstPaneRef(t, source.view.Values)
	servePickerSource(t, client, server.Identity(), source)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		initial := options.Read(ctx, status.Snapshot{})
		require.NoError(t, initial.Err)
		acceptStatusUpdate(initial)
		readCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		dropped := options.Read(readCtx, initial.Values)
		acceptStatusUpdate(dropped)
		nextCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		next := options.Read(nextCtx, dropped.Values)

		require.Error(t, dropped.Err, "nothing changed, so the read timed out")
		require.Equal(t, status.Unknown, dropped.Values.Lookup(ref).State)
		require.NoError(t, next.Err, "a dropped read must not wait for a daemon change")
		require.Equal(t, status.Blocked, next.Values.Lookup(ref).State)
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}

func TestNativePickerDiscardedDeliveryReturnsCurrentView(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	source := newPickerStatusSource(blockedPickerView(t, server.Identity()))
	ref := firstPaneRef(t, source.view.Values)
	servePickerSource(t, client, server.Identity(), source)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	_, err = adapter.Load(t.Context())
	require.NoError(t, err)
	deliveryCtx, stopDelivery := context.WithCancel(t.Context())
	delivery, err := adapter.OpenStatus(deliveryCtx)
	require.NoError(t, err)
	defer func() {
		stopDelivery()
		delivery.clear()
	}()
	read := delivery.readView
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	delivery.readView = func(ctx context.Context, after uint64) (daemon.View, uint64, error) {
		view, sequence, err := read(ctx, after)
		cancel()
		return view, sequence, err
	}

	discarded := delivery.options().Read(ctx, status.Snapshot{})
	acceptStatusUpdate(discarded)
	delivery.readView = read
	nextCtx, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	next := delivery.options().Read(nextCtx, discarded.Values)

	require.NoError(t, discarded.Err)
	require.Zero(t, discarded.Values.Len(), "cancellation during handoff must discard successful evidence")
	require.NoError(t, next.Err)
	require.Equal(t, status.Blocked, next.Values.Lookup(ref).State)
}

func TestNativePickerRecoversFromUnavailableViewWithoutReconnecting(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	source := newPickerStatusSource(blockedPickerView(t, server.Identity()))
	source.unavailable = true
	ref := firstPaneRef(t, source.view.Values)
	servePickerSource(t, client, server.Identity(), source)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		failed := options.Read(ctx, status.Snapshot{})
		acceptStatusUpdate(failed)
		before, err := client.Ping(ctx)
		require.NoError(t, err)

		source.setUnavailable(false)
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		recovered := options.Read(readCtx, failed.Values)

		require.ErrorIs(t, failed.Err, daemon.ErrUnavailable)
		require.NoError(t, recovered.Err)
		require.Equal(t, status.Blocked, recovered.Values.Lookup(ref).State)
		after, err := client.Ping(ctx)
		require.NoError(t, err)
		require.Equal(t, before, after, "an unavailable view keeps the same daemon")
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
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
