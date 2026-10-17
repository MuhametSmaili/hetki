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
	Short: "Update hetki to the latest release or main commit",
	Long: `Update hetki to a verified release or build the latest main commit.

By default installs the newest stable release; prereleases are not supported.
Use --version to pin or downgrade to an exact stable release.
Downloaded binaries are verified against release attestations before replacement.
Use --head to build the latest pushed commit on main (requires Go; unreleased code).
The commit is pinned before building; repeated --head updates compare commits.
GitHub API requests use nonempty GH_TOKEN, then GITHUB_TOKEN, if available.
Tokens are not sent to release downloads or external attestation bundles.
All source builds use the Go checksum database. Emergency-only bypass:
HETKI_UNSAFE_SKIP_VERIFY=1.`,
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&updateHead, "head", false, "Build the latest pushed commit on main (requires Go; unreleased code)")
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "Show what would be done without updating")
	updateCmd.Flags().BoolVarP(&updateVerbose, "verbose", "v", false, "Show verbose output")
	updateCmd.Flags().StringVar(&updateTargetVer, "version", "", "Install this exact stable release tag (e.g. v1.2.3); allows downgrade or reinstall")
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
