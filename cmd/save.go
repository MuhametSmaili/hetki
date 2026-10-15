package cmd

import (
	appsave "github.com/MuhametSmaili/hetki/internal/app/save"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/logger"
	"github.com/spf13/cobra"
)

var saveCmd = &cobra.Command{
	Use:   "save [.]",
	Short: "Save tmux session state to a workspace file",
	Long: `Save tmux session state to a workspace configuration file.

The destination is required: use -n for a named workspace, -p for an
explicit path, or '.' to save into a local .hetki.y[a]ml file. Without
--all, the currently attached session is saved; --all saves every session.`,
	RunE: runSave,
}

var (
	savePath string
	saveName string
	saveAll  bool
)

func init() {
	rootCmd.AddCommand(saveCmd)
	saveCmd.Flags().StringVarP(&savePath, "path", "p", "", "Path to save workspace file")
	saveCmd.Flags().StringVarP(&saveName, "name", "n", "", "Name for the workspace")
	saveCmd.Flags().BoolVar(&saveAll, "all", false, "Save all tmux sessions")

	saveCmd.ValidArgs = []string{"."}
	saveCmd.RegisterFlagCompletionFunc("name", completeWorkspaceNames)
}

func runSave(cmd *cobra.Command, args []string) error {
	service := appsave.NewService(func() (appsave.Backend, error) {
		return detectBackend()
	}, tmux.ValidateAuthoredLayout)

	outputPath, err := service.Run(cmd.Context(), appsave.Options{
		Path:  savePath,
		Name:  saveName,
		Local: len(args) > 0 && args[0] == ".",
		All:   saveAll,
	})
	if err != nil {
		return err
	}

	logger.Success("Saved to %s", outputPath)
	return nil
}
