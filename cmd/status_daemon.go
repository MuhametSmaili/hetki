package cmd

import (
	"github.com/MuhametSmaili/hetki/internal/app/statusobserver"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newStatusDaemonCommand())
}

func newStatusDaemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "__status-daemon server descriptor",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusobserver.ServeDaemon(cmd.Context(), args[0], args[1])
		},
	}
}
