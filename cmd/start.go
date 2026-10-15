package cmd

import (
	appstart "github.com/MuhametSmaili/hetki/internal/app/start"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/spf13/cobra"
)

var (
	startName string
	startPath string
	dryRun    bool
	force     bool
)

var startCmd = &cobra.Command{
	Use:   "start (--name <name> | --path <file>)",
	Short: "Start a tmux workspace",
	Long: `Start a workspace selected by exactly one of --name or --path.
Names select files in Hetki's configured workspace directory, never local files.
Session names come from YAML. Normal start adds missing resources.
Force also replaces mismatched windows and removes extra windows inside declared
sessions. It can terminate their processes, but preserves unrelated sessions.`,
	Example:           "  hetki start -n work\n  hetki start -p ./work.yaml --dry-run",
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              runStart,
}

func init() {
	startCmd.Flags().StringVarP(&startName, "name", "n", "", "Workspace name in Hetki's configured directory")
	startCmd.Flags().StringVarP(&startPath, "path", "p", "", "Workspace YAML file to load (not a directory)")
	startCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Print plan without executing")
	startCmd.Flags().BoolVarP(&force, "force", "f", false, "Reconcile windows inside declared sessions; never remove other sessions")
	startCmd.RegisterFlagCompletionFunc("name", completeWorkspaceNames)
	startCmd.MarkFlagFilename("path", "yaml", "yml")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, _ []string) error {
	service := appstart.NewService(func() (appstart.Backend, error) {
		return detectBackend()
	}, tmux.ValidateAuthoredLayout)

	return service.Run(cmd.Context(), appstart.Options{
		Name:   selectedWorkspaceFlag(cmd, "name", startName),
		Path:   selectedWorkspaceFlag(cmd, "path", startPath),
		DryRun: dryRun,
		Force:  force,
	})
}
