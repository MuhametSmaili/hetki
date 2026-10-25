package cmd

import (
	appsave "github.com/MuhametSmaili/hetki/internal/app/save"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/logger"
	"github.com/spf13/cobra"
)

var saveCmd = &cobra.Command{
	Use:   "save (NAME | --file FILE)",
	Short: "Save tmux sessions to a workspace file",
	Long: `Save tmux sessions to a workspace YAML file.

By default, save the current session from inside tmux and retain other saved sessions.
With --all-sessions, replace the saved session set with all live sessions on the
selected tmux server. Saved sessions that are not running are removed from the file.

Choose either NAME in Hetki's workspace directory or --file FILE.
Save records session structure, not running process state.`,
	Example:           "  hetki save work\n  hetki save --file ./work.yaml --all-sessions",
	Args:              workspaceArgs,
	ValidArgsFunction: completeWorkspaceNames,
	RunE:              runSave,
}

var (
	savePath string
	saveAll  bool
)

func init() {
	rootCmd.AddCommand(saveCmd)
	saveCmd.Flags().StringVarP(&savePath, "file", "f", "", "Save to workspace YAML `FILE`")
	saveCmd.Flags().BoolVarP(&saveAll, "all-sessions", "a", false, "Replace the saved session set with all live sessions")

	cobra.CheckErr(saveCmd.MarkFlagFilename("file", "yaml", "yml"))
}

func runSave(cmd *cobra.Command, args []string) error {
	service := appsave.NewService(func() (appsave.Backend, error) {
		return detectBackend()
	}, tmux.ValidateAuthoredLayout)

	outputPath, err := service.Run(cmd.Context(), appsave.Options{
		Path: selectedWorkspaceFlag(cmd, "file", savePath),
		Name: selectedWorkspaceName(args),
		All:  saveAll,
	})
	if err != nil {
		return err
	}

	logger.Success("Saved to %s", outputPath)
	return nil
}
