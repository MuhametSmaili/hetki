package cmd

import (
	appsave "github.com/MuhametSmaili/hetki/internal/app/save"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/logger"
	"github.com/spf13/cobra"
)

var saveCmd = &cobra.Command{
	Use:   "save (--name <name> | --path <file>)",
	Short: "Save tmux session state to a workspace file",
	Long: `Save a workspace selected by exactly one of --name or --path.
Names select files in Hetki's configured workspace directory, never local files.
Paths identify YAML files, not directories, and can create new files.
Without --all, save updates the current session and retains other saved sessions.
With --all, save replaces the file's session set with all live sessions.`,
	Example:           "  # Replace the example name or path with your own.\n  hetki save -n your_workspace\n  hetki save -p ./path/to/your_workspace.yaml --all",
	Args:              workspaceArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              runSave,
}

var (
	savePath string
	saveName string
	saveAll  bool
)

func init() {
	rootCmd.AddCommand(saveCmd)
	saveCmd.Flags().StringVarP(&savePath, "path", "p", "", "Workspace YAML file to save (not a directory)")
	saveCmd.Flags().StringVarP(&saveName, "name", "n", "", "Workspace name in Hetki's configured directory")
	saveCmd.Flags().BoolVar(&saveAll, "all", false, "Save all tmux sessions")

	cobra.CheckErr(saveCmd.RegisterFlagCompletionFunc("name", completeWorkspaceNames))
	cobra.CheckErr(saveCmd.MarkFlagFilename("path", "yaml", "yml"))
}

func runSave(cmd *cobra.Command, _ []string) error {
	service := appsave.NewService(func() (appsave.Backend, error) {
		return detectBackend()
	}, tmux.ValidateAuthoredLayout)

	outputPath, err := service.Run(cmd.Context(), appsave.Options{
		Path: selectedWorkspaceFlag(cmd, "path", savePath),
		Name: selectedWorkspaceFlag(cmd, "name", saveName),
		All:  saveAll,
	})
	if err != nil {
		return err
	}

	logger.Success("Saved to %s", outputPath)
	return nil
}
