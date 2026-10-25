package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
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

Show the tmux socket, service state, and log path without starting tmux or the service.`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := inspect(cmd.Context())
			if err != nil {
				return err
			}

			return writeServiceStatus(cmd.OutOrStdout(), result)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start the status service",
		Long: `Start the status service for the tmux server selected by your environment.

Requires a running tmux server. An existing compatible service is reused.
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

func writeServiceStatus(output io.Writer, result statusobserver.ServiceStatus) error {
	state := "not listening"
	instance := ""
	if info := result.Daemon.Instance; info != nil {
		state = "running"
		instance = fmt.Sprintf("Daemon PID: %d\nInstance: %s\n", info.PID, terminal.Sanitize(info.Epoch))
	}

	_, err := fmt.Fprintf(output, "Status service: %s\nTmux socket: %s\n%sLog: %s\n",
		state, terminal.Sanitize(result.Server.Socket), instance, terminal.Sanitize(result.Daemon.LogPath))
	return err
}
