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

func TestNativePickerDroppedReadIsQuietAndKeepsSequence(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	source := newPickerStatusSource(blockedPickerView(t, server.Identity()))
	ref := firstPaneRef(t, source.view.Values)
	servePickerSource(t, client, server.Identity(), source)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	service := Service{Options: ui.DefaultOptions(), Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
		initial := runtime.Status.Read(ctx, 0)
		readCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		dropped := runtime.Status.Read(readCtx, initial.Sequence)

		require.NoError(t, initial.Err)
		require.Equal(t, status.Blocked, initial.Values.Lookup(ref).State)
		require.NoError(t, dropped.Err, "a dropped read stays quiet")
		require.Equal(t, initial.Sequence, dropped.Sequence, "a dropped read keeps the sequence")
		return ui.Result{}, nil
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
	_, err = adapter.Load(t.Context(), ui.ViewFlat)
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

	discarded := delivery.options().Read(ctx, 0)
	delivery.readView = read
	nextCtx, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	next := delivery.options().Read(nextCtx, discarded.Sequence)

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
	service := Service{Options: ui.DefaultOptions(), Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
		failed := runtime.Status.Read(ctx, 0)
		before, err := client.Ping(ctx)
		require.NoError(t, err)

		source.setUnavailable(false)
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		recovered := runtime.Status.Read(readCtx, failed.Sequence)

		require.ErrorIs(t, failed.Err, daemon.ErrUnavailable)
		require.NoError(t, recovered.Err)
		require.Equal(t, status.Blocked, recovered.Values.Lookup(ref).State)
		after, err := client.Ping(ctx)
		require.NoError(t, err)
		require.Equal(t, before, after, "an unavailable view keeps the same daemon")
		return ui.Result{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}

func TestNativePickerRecoversAfterDaemonLoss(t *testing.T) {
	b, client, _ := pickerDaemonFixture(t)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	service := Service{Options: ui.DefaultOptions(), Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
		initial := awaitPickerValue(t, ctx, runtime.Status, status.Value{State: status.Idle}, nil)
		before, err := client.Ping(ctx)
		require.NoError(t, err)
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		require.NoError(t, client.Stop(stopCtx))
		require.NoError(t, testenv.WaitUnlocked(stopCtx, strings.TrimSuffix(client.LogPath(), ".log")+".sock.lock"))
		cancel()

		failed := runtime.Status.Read(ctx, initial.Sequence)

		require.Error(t, failed.Err, "loss must be delivered before attempting recovery")
		require.Nil(t, failed.Bindings)
		pane := firstPaneRef(t, initial.Values)
		require.Equal(t, status.Unknown, failed.Values.Lookup(pane).State)
		recovered := awaitPickerValue(t, ctx, runtime.Status, status.Value{State: status.Idle}, nil)
		after, err := client.Ping(ctx)
		require.NoError(t, err)
		require.NotEqual(t, before.Epoch, after.Epoch)
		require.Len(t, recovered.Bindings, 2)
		return ui.Result{}, nil
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
	service := Service{Options: ui.DefaultOptions(), Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
		failed := runtime.Status.Read(ctx, 0)
		require.Error(t, failed.Err)

		require.NoError(t, os.WriteFile(path, profile, 0600))
		recovered := awaitPickerValue(t, ctx, runtime.Status, status.Value{State: status.Idle}, nil)

		require.Len(t, recovered.Bindings, 2, "the same reader must recover both linked occurrences")
		return ui.Result{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}
