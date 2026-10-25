package cmd

import (
	appstart "github.com/MuhametSmaili/hetki/internal/app/start"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/spf13/cobra"
)

var (
	startPath     string
	dryRun        bool
	detach        bool
	force         bool
	pruneSessions bool
)

var startCmd = &cobra.Command{
	Use:   "start (NAME | --file FILE)",
	Short: "Start a saved workspace",
	Long: `Start a saved workspace.

Create missing sessions and windows with their configured panes.
Leave existing windows unchanged unless --force is used.

Attach to the first session listed in the workspace. Inside tmux, switch to it instead.

Choose either NAME from Hetki's workspace directory or --file FILE.

Caution:
  --force replaces mismatched windows and removes extra windows in listed sessions.
  --prune-sessions deletes sessions absent from the workspace on the selected
  tmux server, including unrelated sessions.
  Both options can terminate running processes. Use --dry-run first.
  If execution fails, earlier changes are not rolled back.`,
	Example:           "  hetki start work\n  hetki start --file ./work.yaml --dry-run",
	Args:              workspaceArgs,
	ValidArgsFunction: completeWorkspaceNames,
	RunE:              runStart,
}

func init() {
	startCmd.Flags().StringVarP(&startPath, "file", "f", "", "Load workspace YAML `FILE`")
	startCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Show planned changes without applying them")
	startCmd.Flags().BoolVarP(&detach, "detach", "d", false, "Do not attach or switch sessions")
	startCmd.Flags().BoolVar(&force, "force", false, "Replace mismatched windows and delete extra windows in listed sessions")
	startCmd.Flags().BoolVar(&pruneSessions, "prune-sessions", false, "Delete sessions not listed in the workspace")
	cobra.CheckErr(startCmd.MarkFlagFilename("file", "yaml", "yml"))
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	service := appstart.NewService(func() (appstart.Backend, error) {
		return detectBackend()
	}, tmux.ValidateAuthoredLayout)

	return service.Run(cmd.Context(), appstart.Options{
		Name:          selectedWorkspaceName(args),
		Path:          selectedWorkspaceFlag(cmd, "file", startPath),
		DryRun:        dryRun,
		Detach:        detach,
		Force:         force,
		PruneSessions: pruneSessions,
	})
}
