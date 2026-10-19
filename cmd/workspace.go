package cmd

import (
	"fmt"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/spf13/cobra"
)

func workspaceArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return appshared.WithHint(fmt.Errorf("%s does not accept positional arguments", cmd.Name()),
		fmt.Sprintf("Use hetki %s -n your_workspace or hetki %s -p ./path/to/your_workspace.yaml. Replace the example name or path with your own.", cmd.Name(), cmd.Name()))
}

func selectedWorkspaceFlag(cmd *cobra.Command, name, value string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}
