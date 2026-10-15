package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/logger"
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
	Short:         "hetki - Terminal Multiplexer Session Manager",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.NoArgs,
	RunE:          runBareHetki,
	Long: `hetki is a terminal multiplexer session manager that helps you manage complex tmux sessions with less manual setup.

It supports:
- Multiple sessions and windows with panes
- Strict YAML configuration files
- Named and local workspaces
- Templates for reusable configurations`,
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
	stop()
	if err != nil {
		logger.Error("%v", err)
		os.Exit(1)
	}
}
