package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"

	detectBackend = backend.Detect
)

var rootCmd = &cobra.Command{
	Use:           "hetki",
	Short:         "Manage tmux sessions and saved workspaces",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.NoArgs,
	RunE:          runBareHetki,
	Long: `Manage tmux sessions and saved workspaces.

Run hetki in a terminal to open the interactive picker.
Save tmux sessions as YAML workspaces and start them again.`,
	Example: "  hetki\n  hetki start work\n  hetki save --file ./work.yaml",
	Version: Version,
}

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		applyBuildInfo(info)
	}
	rootCmd.Version = Version
	rootCmd.Flags().BoolP("version", "V", false, "Show version information")
	rootCmd.SetVersionTemplate(fmt.Sprintf("hetki version %s\ncommit: %s\nbuilt: %s\n", Version, GitCommit, BuildDate))
}

func applyBuildInfo(info *debug.BuildInfo) {
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	if GitCommit != "unknown" {
		return
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			GitCommit = setting.Value
			return
		}
	}
}

func commandSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func Execute() {
	ctx, stop := commandSignalContext()
	err := rootCmd.ExecuteContext(ctx)
	reportCommandError(ctx, err)
	stop()
	if err != nil {
		os.Exit(1)
	}
}
