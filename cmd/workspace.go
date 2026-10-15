package cmd

import "github.com/spf13/cobra"

func selectedWorkspaceFlag(cmd *cobra.Command, name, value string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}
