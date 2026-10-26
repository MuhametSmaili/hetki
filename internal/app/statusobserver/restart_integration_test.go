//go:build integration

package statusobserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/buildinfo"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/stretchr/testify/require"
)

type restartSource struct {
	close func() error
}

func (s restartSource) Changes() <-chan struct{} { return nil }

func (s restartSource) ReadView(context.Context) (daemon.View, error) {
	return daemon.View{}, nil
}

func (s restartSource) Close() error { return s.close() }

func restartDaemonFixture(t *testing.T, server observe.Server, closeSource func() error) (*daemon.Client, <-chan struct{}) {
	t.Helper()
	client := daemonClientFixture(t, server.Identity())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var serveErr error
	t.Cleanup(func() {
		cancel()
		await(t, done)
		require.NoError(t, serveErr)
	})
	go func() {
		defer close(done)
		serveErr = daemon.Serve(ctx, daemon.Binding{
			Server: server.Identity(),
			Build:  buildinfo.Identity(),
			Alive:  func(ctx context.Context) bool { return server.Validate(ctx) == nil },
		}, func(context.Context, func(error)) (daemon.Source, error) {
			return restartSource{close: closeSource}, nil
		})
	}()
	readyCtx, readyCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer readyCancel()
	require.Eventually(t, func() bool {
		_, err := client.Ping(readyCtx)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)
	return client, done
}

func TestNativeRestartReportsStartupFailureAfterStop(t *testing.T) {
	executable := buildDaemonExecutable(t)
	_, server, _, _, _ := nativeStreamsFixture(t)
	t.Setenv("TMUX", fmt.Sprintf("%s,%d,0", server.Socket, server.PID))
	profileDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "hetki", "status.d")
	require.NoError(t, os.MkdirAll(profileDir, 0700))
	client, done := restartDaemonFixture(t, server, func() error {
		return os.WriteFile(filepath.Join(profileDir, "changed.yaml"), []byte("version: invalid\n"), 0600)
	})

	output, err := runServiceCommand(t, executable, "restart")

	require.Error(t, err, "%s", output)
	require.Contains(t, string(output), "start status service after stop")
	require.Contains(t, string(output), "changed.yaml")
	require.NotContains(t, string(output), "Status service is running.")
	await(t, done)
	_, err = client.Ping(t.Context())
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, server.Validate(t.Context()))
}

func TestNativeRestartCancellationDuringCleanupPreventsStartup(t *testing.T) {
	_, server, _, _, _ := nativeStreamsFixture(t)
	t.Setenv("TMUX", fmt.Sprintf("%s,%d,0", server.Socket, server.PID))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client, done := restartDaemonFixture(t, server, func() error {
		cancel()
		return nil
	})

	err := RestartService(ctx)

	require.ErrorIs(t, err, context.Canceled)
	await(t, done)
	_, err = client.Ping(t.Context())
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, server.Validate(t.Context()))
}
