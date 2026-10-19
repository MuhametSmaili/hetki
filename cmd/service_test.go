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
			}, nil, nil, nil)
			command.SetOut(&output)
			command.SetArgs([]string{"status"})

			err := command.ExecuteContext(t.Context())

			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.want, output.String())
		})
	}
}

func TestServiceHelpAndInvalidArgumentsDoNotRunActions(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		fail bool
	}{
		{"group without subcommand", nil, false},
		{"group help", []string{"--help"}, false},
		{"status help", []string{"status", "--help"}, false},
		{"start help", []string{"start", "--help"}, false},
		{"stop help", []string{"stop", "--help"}, false},
		{"extra status argument", []string{"status", "another-server"}, true},
		{"extra start argument", []string{"start", "another-server"}, true},
		{"extra stop argument", []string{"stop", "another-server"}, true},
		{"unknown stop flag", []string{"stop", "--force"}, true},
		{"restart help", []string{"restart", "--help"}, false},
		{"extra restart argument", []string{"restart", "another-server"}, true},
		{"unknown restart flag", []string{"restart", "--force"}, true},
		{"unknown command", []string{"unknown"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
				t.Fatal("help and invalid arguments must not inspect the service")
				return statusobserver.ServiceStatus{}, nil
			}, func(context.Context) error {
				t.Fatal("help and invalid arguments must not start the service")
				return nil
			}, func(context.Context) (*daemon.Info, error) {
				t.Fatal("help and invalid arguments must not stop the service")
				return nil, nil
			}, func(context.Context) error {
				t.Fatal("help and invalid arguments must not restart the service")
				return nil
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

func TestServiceStartUsesExistingStartupAction(t *testing.T) {
	failure := errors.New("fixture startup failed")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"running", nil, "Status service is running.\n"},
		{"startup failure", failure, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
				t.Fatal("start must not add an inspection before or after the startup action")
				return statusobserver.ServiceStatus{}, nil
			}, func(ctx context.Context) error {
				calls++
				require.Equal(t, t.Context(), ctx)
				return tc.err
			}, func(context.Context) (*daemon.Info, error) {
				t.Fatal("start must not stop the service")
				return nil, nil
			}, nil)
			var output bytes.Buffer
			command.SetOut(&output)
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetArgs([]string{"start"})

			err := command.ExecuteContext(t.Context())

			require.ErrorIs(t, err, tc.err)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.want, output.String())
		})
	}
}

func TestServiceRestartUsesRestartAction(t *testing.T) {
	for _, failure := range []error{nil, errors.New("restart failed"), context.Canceled, context.DeadlineExceeded} {
		name := "running"
		if failure != nil {
			name = failure.Error()
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			command := newServiceCommand(nil, nil, nil, func(ctx context.Context) error {
				calls++
				require.Equal(t, t.Context(), ctx)
				return failure
			})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetArgs([]string{"restart"})

			err := command.ExecuteContext(t.Context())

			require.ErrorIs(t, err, failure)
			require.Equal(t, 1, calls)
			if failure == nil {
				require.Equal(t, "Status service is running.\n", output.String())
			} else {
				require.Empty(t, output.String())
			}
		})
	}
}

func TestServiceStopUsesStopAction(t *testing.T) {
	failure := errors.New("fixture stop failed")
	for _, tc := range []struct {
		name string
		info *daemon.Info
		err  error
		want string
	}{
		{"stopped", &daemon.Info{Epoch: "fixture"}, nil, "Stopped status service instance fixture.\n"},
		{"not listening", nil, nil, "Status service is not listening.\n"},
		{"stop failure", nil, failure, ""},
		{"observed instance with failure", &daemon.Info{Epoch: "fixture"}, failure, ""},
		{"canceled", nil, context.Canceled, ""},
		{"timed out", nil, context.DeadlineExceeded, ""},
		{"sanitized epoch", &daemon.Info{Epoch: "fixture\x1b[31m\n\t"}, nil, "Stopped status service instance fixture\\n\\t.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
				t.Fatal("stop must not inspect the service separately")
				return statusobserver.ServiceStatus{}, nil
			}, func(context.Context) error {
				t.Fatal("stop must not start the service")
				return nil
			}, func(ctx context.Context) (*daemon.Info, error) {
				calls++
				require.Equal(t, t.Context(), ctx)
				return tc.info, tc.err
			}, nil)
			var output bytes.Buffer
			command.SetOut(&output)
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetArgs([]string{"stop"})

			err := command.ExecuteContext(t.Context())

			require.ErrorIs(t, err, tc.err)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.want, output.String())
		})
	}
}

func TestServiceStatusPreservesInspectionFailure(t *testing.T) {
	failure := errors.New("fixture inspection failed")
	command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
		return statusobserver.ServiceStatus{}, failure
	}, nil, nil, nil)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs([]string{"status"})

	err := command.ExecuteContext(t.Context())

	require.ErrorIs(t, err, failure)
	require.Empty(t, output.String(), "an inspection error must not print a successful status")
}

func TestServiceCommandsPreserveOutputFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		info   *daemon.Info
	}{
		{"status", "status", nil},
		{"start", "start", nil},
		{"restart", "restart", nil},
		{"stop instance", "stop", &daemon.Info{Epoch: "fixture"}},
		{"stop not listening", "stop", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			require.NoError(t, reader.Close())
			defer writer.Close()
			command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
				return statusobserver.ServiceStatus{}, nil
			}, func(context.Context) error { return nil }, func(context.Context) (*daemon.Info, error) {
				return tc.info, nil
			}, func(context.Context) error { return nil })
			command.SetOut(writer)
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetArgs([]string{tc.action})

			err := command.ExecuteContext(t.Context())

			require.ErrorIs(t, err, io.ErrClosedPipe)
		})
	}
}

func TestServiceStatusSanitizesPaths(t *testing.T) {
	command := newServiceCommand(func(context.Context) (statusobserver.ServiceStatus, error) {
		return statusobserver.ServiceStatus{
			Server: observe.Server{Socket: "/tmp/tmux\x1b[31m\n.sock"},
			Daemon: daemon.Inspection{LogPath: "/tmp/status\t.log"},
		}, nil
	}, nil, nil, nil)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"status"})

	err := command.ExecuteContext(t.Context())

	require.NoError(t, err)
	require.Equal(t, "Status service: not listening\nTmux socket: /tmp/tmux\\n.sock\nLog: /tmp/status\\t.log\n", output.String())
}
