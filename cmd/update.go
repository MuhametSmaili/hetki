package cmd

import (
	appupdate "github.com/MuhametSmaili/hetki/internal/app/update"
	"github.com/spf13/cobra"
)

var (
	updateHead      bool
	updateDryRun    bool
	updateVerbose   bool
	updateTargetVer string
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Hetki",
	Long: `Update Hetki to the latest stable release.

Use --version to select an exact stable release, or --head for unreleased main.
These options cannot be combined. --head requires Go.
Downloaded release binaries are verified before replacement.`,
	Example:           "  hetki update\n  hetki update --dry-run",
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&updateHead, "head", false, "Build the latest pushed commit on main (requires Go; unreleased code)")
	updateCmd.Flags().BoolVarP(&updateDryRun, "dry-run", "n", false, "Preview the update without installing it")
	updateCmd.Flags().BoolVarP(&updateVerbose, "verbose", "v", false, "Show verbose output")
	updateCmd.Flags().StringVar(&updateTargetVer, "version", "", "Install exact stable release `TAG` (allows downgrade or reinstall)")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	return appupdate.NewService().Run(cmd.Context(), appupdate.Options{
		CurrentVersion: Version,
		CurrentCommit:  GitCommit,
		Head:           updateHead,
		TargetVersion:  updateTargetVer,
		DryRun:         updateDryRun,
		Verbose:        updateVerbose,
	})
}
