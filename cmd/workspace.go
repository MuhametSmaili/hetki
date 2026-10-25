package cmd

import (
	"fmt"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/spf13/cobra"
)

func workspaceArgs(cmd *cobra.Command, args []string) error {
	if len(args) <= 1 {
		return nil
	}
	return appshared.WithHint(fmt.Errorf("%s accepts at most one workspace name", cmd.Name()),
		fmt.Sprintf("Use hetki %s work or hetki %s --file ./work.yaml.", cmd.Name(), cmd.Name()))
}

func selectedWorkspaceName(args []string) *string {
	if len(args) == 0 {
		return nil
	}
	return &args[0]
}

func selectedWorkspaceFlag(cmd *cobra.Command, name, value string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}
