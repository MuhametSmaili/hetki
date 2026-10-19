//go:build integration

package statusobserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/stretchr/testify/require"
)

func runServiceStatusCommand(t *testing.T, executable string) ([]byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, "service", "status")
	command.WaitDelay = 100 * time.Millisecond
	return command.CombinedOutput()
}

func TestNativeServiceStatus(t *testing.T) {
	executable := buildDaemonExecutable(t)

	t.Run("selected detached server without a daemon", func(t *testing.T) {
		_, server, _, _, _ := nativeStreamsFixture(t)
		t.Setenv("TMUX", fmt.Sprintf("%s,%d,0", server.Socket, server.PID))
		t.Setenv("TMUX_TMPDIR", os.Getenv("HOME"))
		writeConfiguredProfile(t, "bad.yaml", "version: invalid\n")

		result, err := InspectService(t.Context())
		output, commandErr := runServiceStatusCommand(t, executable)

		require.NoError(t, err)
		require.Equal(t, server, result.Server)
		require.Nil(t, result.Daemon.Instance)
		require.NoError(t, commandErr, "%s", output)
		require.Equal(t, fmt.Sprintf("Status service: not listening\nTmux socket: %s\nLog: %s\n", server.Socket, result.Daemon.LogPath), string(output))
		_, err = os.Lstat(filepath.Dir(result.Daemon.LogPath))
		require.ErrorIs(t, err, os.ErrNotExist, "inspection must not prepare daemon storage")
	})

	t.Run("default selection and no fallback from a missing selected server", func(t *testing.T) {
		selected, server, _, _, _ := nativeStreamsFixture(t)
		t.Setenv("TMUX", "")
		t.Setenv("TMUX_TMPDIR", os.Getenv("HOME"))
		dir := filepath.Join(os.Getenv("HOME"), fmt.Sprintf("tmux-%d", os.Getuid()))
		require.NoError(t, os.Mkdir(dir, 0700))
		fallback := nativeRunner{client: selected.client, socket: filepath.Join(dir, "default"), stopped: new(atomic.Bool)}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			require.NoError(t, fallback.stop(ctx))
		})
		_, err := fallback.RunLimited(t.Context(), 4096, "new-session", "-d", "-s", "default", "cat")
		require.NoError(t, err)
		defaultServer, err := observe.ResolveServer(t.Context(), fallback)
		require.NoError(t, err)

		result, err := InspectService(t.Context())

		require.NoError(t, err)
		require.Equal(t, defaultServer, result.Server)
		require.Nil(t, result.Daemon.Instance)

		t.Setenv("TMUX", fmt.Sprintf("%s,%d,0", server.Socket, server.PID))
		require.NoError(t, selected.stop(t.Context()))

		_, err = InspectService(t.Context())

		require.Error(t, err, "a missing selected server must not silently select the live default server")
		require.NoError(t, defaultServer.Validate(t.Context()))
	})

	t.Run("existing daemon keeps its instance", func(t *testing.T) {
		_, server, _, _, _ := nativeStreamsFixture(t)
		t.Setenv("TMUX", fmt.Sprintf("%s,%d,0", server.Socket, server.PID))
		t.Setenv("TMUX_TMPDIR", os.Getenv("HOME"))
		client := daemonClientFixture(t, server.Identity())
		_, err := EnsureDaemon(t.Context(), server, executable)
		require.NoError(t, err)
		before, err := client.Ping(t.Context())
		require.NoError(t, err)
		writeConfiguredProfile(t, "bad.yaml", "version: invalid\n")

		result, err := InspectService(t.Context())
		output, commandErr := runServiceStatusCommand(t, executable)
		after, pingErr := client.Ping(t.Context())

		require.NoError(t, err)
		require.Equal(t, server, result.Server)
		require.NotNil(t, result.Daemon.Instance)
		require.Equal(t, before, *result.Daemon.Instance)
		require.NoError(t, commandErr, "%s", output)
		require.Contains(t, string(output), "Status service: running\n")
		require.Contains(t, string(output), fmt.Sprintf("Daemon PID: %d\nInstance: %s\n", before.PID, before.Epoch))
		require.NoError(t, pingErr)
		require.Equal(t, before, after)
	})

	t.Run("stopped tmux server is not started", func(t *testing.T) {
		runner, server, _, _, _ := nativeStreamsFixture(t)
		t.Setenv("TMUX", fmt.Sprintf("%s,%d,0", server.Socket, server.PID))
		t.Setenv("TMUX_TMPDIR", os.Getenv("HOME"))
		require.NoError(t, runner.stop(t.Context()))

		_, err := InspectService(t.Context())
		output, commandErr := runServiceStatusCommand(t, executable)

		require.Error(t, err)
		var exit *exec.ExitError
		require.ErrorAs(t, commandErr, &exit, "%s", output)
		require.Equal(t, 1, exit.ExitCode())
		require.NotContains(t, string(output), "Status service: running")
		_, queryErr := runner.RunLimited(t.Context(), 4096, "list-sessions")
		require.Error(t, queryErr, "inspection must not recreate the selected tmux server")
		result, inspectErr := daemon.Inspect(t.Context(), server.Identity())
		require.NoError(t, inspectErr)
		_, statErr := os.Lstat(filepath.Dir(result.LogPath))
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})
}
