package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/buildinfo"
	"github.com/spf13/cobra"
)

var detectBackend = backend.Detect

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
	Version: buildinfo.Version(),
}

func init() {
	rootCmd.Flags().BoolP("version", "V", false, "Show version information")
	rootCmd.SetVersionTemplate(fmt.Sprintf("hetki version %s\ncommit: %s\nbuilt: %s\n", buildinfo.Version(), buildinfo.Commit(), buildinfo.Date()))
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
