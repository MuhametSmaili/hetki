package cmd

import (
	appstart "github.com/MSmaili/hetki/internal/app/start"
	"github.com/MSmaili/hetki/internal/backend/tmux"
	"github.com/spf13/cobra"
)

var (
	dryRun bool
	force  bool
)

var startCmd = &cobra.Command{
	Use:   "start [workspace-name-or-path]",
	Short: "Start a tmux workspace",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Print plan without executing")
	startCmd.Flags().BoolVarP(&force, "force", "f", false, "Reconcile windows inside declared sessions; never remove other sessions")
	rootCmd.AddCommand(startCmd)

	startCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeWorkspaceNames(cmd, args, toComplete)
	}
}

func runStart(cmd *cobra.Command, args []string) error {
	var nameOrPath string
	if len(args) > 0 {
		nameOrPath = args[0]
	}

	service := appstart.NewService(func() (appstart.Backend, error) {
		return detectBackend()
	}, tmux.ValidateAuthoredLayout)

	return service.Run(cmd.Context(), appstart.Options{
		Workspace: nameOrPath,
		DryRun:    dryRun,
		Force:     force,
	})
}
