package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/stretchr/testify/require"
)

func TestServiceStatusOutput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		instance *daemon.Info
		want     string
	}{
		{"not listening", nil, "Status service: not listening\nTmux socket: /tmp/tmux.sock\nLog: /tmp/status.log\n"},
		{"running", &daemon.Info{PID: 123, Epoch: "fixture"}, "Status service: running\nTmux socket: /tmp/tmux.sock\nDaemon PID: 123\nInstance: fixture\nLog: /tmp/status.log\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			calls := 0
			command := newServiceCommand(func(ctx context.Context) (statusobserver.ServiceStatus, error) {
				calls++
				require.Equal(t, t.Context(), ctx)
				return statusobserver.ServiceStatus{
					Server: observe.Server{Socket: "/tmp/tmux.sock"},
					Daemon: daemon.Inspection{Instance: tc.instance, LogPath: "/tmp/status.log"},
				}, nil
			})
			command.SetOut(&output)
			command.SetArgs([]string{"status"})

			err := command.ExecuteContext(t.Context())

			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.want, output.String())
		})
	}
}

func TestServiceHelpAndInvalidArgumentsDoNotInspect(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		fail bool
	}{
		{"group without subcommand", nil, false},
		{"group help", []string{"--help"}, false},
		{"status help", []string{"status", "--help"}, false},
		{"extra argument", []string{"status", "another-server"}, true},
		{"unknown command", []string{"unknown"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
				t.Fatal("help and invalid arguments must not inspect the service")
				return statusobserver.ServiceStatus{}, nil
			})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetArgs(tc.args)

			err := command.ExecuteContext(t.Context())

			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Contains(t, output.String(), "status")
			}
		})
	}
}

func TestServiceStatusPreservesInspectionFailure(t *testing.T) {
	failure := errors.New("fixture inspection failed")
	command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
		return statusobserver.ServiceStatus{}, failure
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs([]string{"status"})

	err := command.ExecuteContext(t.Context())

	require.ErrorIs(t, err, failure)
	require.Empty(t, output.String(), "an inspection error must not print a successful status")
}

func TestServiceStatusPreservesOutputFailure(t *testing.T) {
	reader, writer := io.Pipe()
	require.NoError(t, reader.Close())
	defer writer.Close()
	command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
		return statusobserver.ServiceStatus{}, nil
	})
	command.SetOut(writer)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs([]string{"status"})

	err := command.ExecuteContext(t.Context())

	require.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestServiceStatusSanitizesPaths(t *testing.T) {
	command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
		return statusobserver.ServiceStatus{
			Server: observe.Server{Socket: "/tmp/tmux\x1b[31m\n.sock"},
			Daemon: daemon.Inspection{LogPath: "/tmp/status\t.log"},
		}, nil
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"status"})

	err := command.ExecuteContext(t.Context())

	require.NoError(t, err)
	require.Equal(t, "Status service: not listening\nTmux socket: /tmp/tmux\\n.sock\nLog: /tmp/status\\t.log\n", output.String())
}
