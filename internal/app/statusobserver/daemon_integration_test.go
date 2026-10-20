//go:build integration

package statusobserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestNativeExecutableDaemonLifecycle(t *testing.T) {
	executable := buildDaemonExecutable(t)

	t.Run("reuse and stop", func(t *testing.T) {
		runner, server, _, _, _ := nativeStreamsFixture(t)
		client := daemonClientFixture(t, server.Identity())
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		launchCtx, releaseLauncher := context.WithCancel(ctx)
		_, err := EnsureDaemon(launchCtx, server, executable)
		releaseLauncher()
		require.NoError(t, err)
		first, err := client.Ping(ctx)
		require.NoError(t, err)
		awaitDaemonSubscription(t, ctx, runner, first.PID)
		_, err = EnsureDaemon(ctx, server, executable)
		require.NoError(t, err)
		second, err := client.Ping(ctx)
		require.NoError(t, err)
		snapshot, err := readDaemonStatus(ctx, client)
		require.NoError(t, err)

		require.Equal(t, first, second)
		require.Zero(t, snapshot.Len(), "raw terminal output must not fabricate status")
		require.NoError(t, client.Stop(ctx))
		awaitDaemonStopped(t, client)
		data, err := os.ReadFile(client.LogPath())
		require.NoError(t, err)
		require.Equal(t, 1, strings.Count(string(data), `event="starting"`))
		require.Contains(t, string(data), `event="stopped"`)
	})

	t.Run("native server exit", func(t *testing.T) {
		runner, server, _, _, _ := nativeStreamsFixture(t)
		client := daemonClientFixture(t, server.Identity())
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		_, err := EnsureDaemon(ctx, server, executable)
		require.NoError(t, err)
		info, err := client.Ping(ctx)
		require.NoError(t, err)
		awaitDaemonSubscription(t, ctx, runner, info.PID)

		require.NoError(t, runner.stop(ctx))
		awaitDaemonStopped(t, client)
		_, err = EnsureDaemon(ctx, server, executable)

		require.Error(t, err, "a dead incarnation cannot be relaunched")
	})

	t.Run("unsupported version is logged before readiness", func(t *testing.T) {
		_, server, _, _, _ := nativeStreamsFixture(t)
		client := daemonClientFixture(t, server.Identity())
		server.Version = "unsupported"

		_, err := EnsureDaemon(t.Context(), server, executable)
		data, readErr := os.ReadFile(client.LogPath())

		require.ErrorContains(t, err, "exited before readiness")
		require.NoError(t, readErr)
		require.Contains(t, string(data), `event="serve_failed"`)
		require.Contains(t, string(data), "create status sampler")
	})

	t.Run("terminal source failure stops serving", func(t *testing.T) {
		_, server, _, _, _ := nativeStreamsFixture(t)
		client := daemonClientFixture(t, server.Identity())
		if server.Version == "3.2" {
			server.Version = "3.8"
		} else {
			server.Version = "3.2"
		}
		descriptor, err := json.Marshal(server)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		err = ServeDaemon(ctx, server.Identity(), string(descriptor))
		data, readErr := os.ReadFile(client.LogPath())

		require.ErrorIs(t, err, observe.ErrServerChanged)
		require.NoError(t, ctx.Err(), "source failure must terminate before the caller deadline")
		require.NoError(t, readErr)
		require.Contains(t, string(data), `event="source_failed"`)
		require.Contains(t, string(data), `event="serve_failed"`)
	})
}

func buildDaemonExecutable(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "hetki")
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "go", "build", "-race", "-o", path, "../../..")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return path
}

func daemonClientFixture(t *testing.T, identity string) *daemon.Client {
	t.Helper()
	client, err := daemon.Connect(identity)
	require.NoError(t, err)
	relative, err := filepath.Rel(os.Getenv("HOME"), client.LogPath())
	require.NoError(t, err)
	require.True(t, filepath.IsLocal(relative), "daemon files must stay in the private test home")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := client.Stop(ctx); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			t.Errorf("stop daemon fixture: %v", err)
		}
		awaitDaemonStopped(t, client)
	})
	return client
}

func awaitDaemonSubscription(t *testing.T, ctx context.Context, runner nativeRunner, pid int) {
	t.Helper()

	want := fmt.Sprintf("%d|1", pid)
	require.Eventually(t, func() bool {
		out, err := runner.RunLimited(ctx, 4096, "list-clients", "-F", "#{client_pid}|#{client_control_mode}")
		return err == nil && strings.Contains(out, want)
	}, 3*time.Second, 10*time.Millisecond)
}

func awaitDaemonStopped(t *testing.T, client *daemon.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	path := strings.TrimSuffix(client.LogPath(), ".log") + ".sock.lock"
	if err := testenv.WaitUnlocked(ctx, path); err != nil {
		t.Errorf("daemon fixture teardown: %v", err)
	}
}
