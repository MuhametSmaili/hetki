package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newServiceCommand(statusobserver.InspectService, statusobserver.StartService))
}

func newServiceCommand(
	inspect func(context.Context) (statusobserver.ServiceStatus, error),
	start func(context.Context) error,
) *cobra.Command {
	command := &cobra.Command{
		Use:               "service",
		Short:             "Manage the status service for the selected tmux server",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show status without starting tmux or the service",
		Long: `Inspect the status service for the tmux server selected by your environment.
This command does not start tmux or a daemon, create Hetki runtime directories,
or load status profiles. Detached tmux sessions are supported.
A missing or refused endpoint means not listening, not confirmed process exit.
Build and uptime are not reported by the current daemon protocol.`,
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
		Short: "Start or reuse the status service without opening the picker",
		Long: `Start or reuse the status service for the selected, running tmux server.
This command never starts tmux or waits for a future tmux server.
The daemon runs independently after this command exits and stops with its server.
An existing compatible daemon keeps its loaded profiles.`,
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
