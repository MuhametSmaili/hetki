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
	Short:         "Manage tmux workspaces from a keyboard-first picker or the CLI",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.NoArgs,
	RunE:          runBareHetki,
	Long: `Hetki manages tmux sessions, windows, and panes.

Run hetki in a terminal to open the interactive picker.
Use start or save with exactly one workspace selector: --name or --path.
Workspaces use strict YAML. A workspace name selects a file, not session ownership.`,
	Example: "  hetki\n  hetki start -n work\n  hetki save -p ./work.yaml",
	Version: Version,
}

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		applyBuildInfo(info)
	}
	rootCmd.Version = Version
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
