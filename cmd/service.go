package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	"github.com/MuhametSmaili/hetki/internal/buildinfo"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newServiceCommand(statusobserver.InspectService, statusobserver.StartService, statusobserver.StopService, statusobserver.RestartService))
}

func newServiceCommand(
	inspect func(context.Context) (statusobserver.ServiceStatus, error),
	start func(context.Context) error,
	stop func(context.Context) (*daemon.Info, error),
	restart func(context.Context) error,
) *cobra.Command {
	command := &cobra.Command{
		Use:   "service",
		Short: "Manage the background status service",
		Long: `Manage the background service that tracks application status in tmux panes.

Commands use the tmux server selected by your environment.
The picker starts the service automatically.`,
		Example:           "  hetki service status\n  hetki service restart",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show status service information",
		Long: `Show status service information for the tmux server selected by your environment.

Show the tmux socket, service state, build, and log path without starting tmux or the service.
The state is running, running another hetki build, not listening, or starting or stopping.
The picker and restart replace a service from another hetki build.`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := inspect(cmd.Context())
			if err != nil {
				return err
			}

			return writeServiceStatus(cmd.OutOrStdout(), result, buildinfo.Identity())
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start the status service",
		Long: `Start the status service for the tmux server selected by your environment.

Requires a running tmux server. An existing service of this hetki build is reused.
A service of another build is stopped and replaced.
The service runs in the background and stops when its tmux server exits.`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := start(cmd.Context()); err != nil {
				return err
			}

			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Status service is running.")
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the status service without stopping tmux",
		Long: `Stop the status service for the tmux server selected by your environment.

Requires a running tmux server. Tmux sessions remain running.
If the service is starting or stopping, waits briefly for it to finish first.
A service of another hetki protocol receives SIGTERM after its identity is confirmed.
An open picker can start the service again.
Cancellation or timeout does not undo an accepted shutdown request.`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, err := stop(cmd.Context())
			if err != nil {
				return err
			}

			if info == nil {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Status service is not listening.")
			} else {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Stopped status service instance %s.\n", terminal.Sanitize(info.Epoch))
			}
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Restart the status service",
		Long: `Restart the status service for the tmux server selected by your environment.

Requires a running tmux server. If the service is absent, this command starts it.
Invalid profiles leave the existing service running.
A failure after shutdown acceptance can leave the service stopped.`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := restart(cmd.Context()); err != nil {
				return err
			}

			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Status service is running.")
			return err
		},
	})
	return command
}

func writeServiceStatus(output io.Writer, result statusobserver.ServiceStatus, currentBuild string) error {
	state, instance, err := describeServiceState(result.Daemon, currentBuild)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(output, "Status service: %s\nTmux socket: %s\n%sLog: %s\n",
		state, terminal.Sanitize(result.Server.Socket), instance, terminal.Sanitize(result.Daemon.LogPath))
	return err
}

// describeServiceState returns the state label and, only for a running
// service, its instance lines.
func describeServiceState(inspection daemon.Inspection, currentBuild string) (string, string, error) {
	switch inspection.State {
	case daemon.Absent:
		return "not listening", "", nil
	case daemon.Transition:
		return "starting or stopping", "", nil
	case daemon.Running, daemon.Incompatible:
		return describeInstance(inspection, currentBuild)
	default:
		return "", "", fmt.Errorf("unknown status service state %d", inspection.State)
	}
}

func describeInstance(inspection daemon.Inspection, currentBuild string) (string, string, error) {
	info := inspection.Instance
	if info == nil {
		return "", "", errors.New("status service inspection reported an instance state without an instance")
	}
	lines := fmt.Sprintf("Daemon PID: %d\nInstance: %s\n", info.PID, terminal.Sanitize(info.Epoch))

	if inspection.State == daemon.Incompatible {
		return "running another hetki build", lines + fmt.Sprintf("Protocol: %d\n", inspection.Protocol), nil
	}

	build := terminal.Sanitize(info.Build)
	if info.Build != currentBuild {
		build += " (differs from this hetki: " + terminal.Sanitize(currentBuild) + ")"
	}
	lines += "Build: " + build + "\n"
	if info.Executable != "" {
		lines += "Executable: " + terminal.Sanitize(info.Executable) + "\n"
	}
	return "running", lines, nil
}
