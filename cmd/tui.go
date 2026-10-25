package cmd

import (
	"context"
	"os"

	apptui "github.com/MuhametSmaili/hetki/internal/app/tui"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var (
	isTerminal = term.IsTerminal
	openTUI    = func(ctx context.Context, overrides apptui.Overrides) error {
		return apptui.Open(ctx, func() (apptui.Backend, error) {
			return detectBackend()
		}, overrides)
	}
)

func runBareHetki(cmd *cobra.Command, _ []string) error {
	overrides, err := pickerOverrides(cmd)
	if err != nil {
		return err
	}
	if !isTerminal(os.Stdin.Fd()) || !isTerminal(os.Stdout.Fd()) {
		return cmd.Help()
	}
	return openTUI(cmd.Context(), overrides)
}
