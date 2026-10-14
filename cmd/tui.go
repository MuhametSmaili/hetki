package cmd

import (
	"context"
	"os"

	apptui "github.com/MSmaili/hetki/internal/app/tui"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var (
	isTerminal = term.IsTerminal
	openTUI    = func(ctx context.Context) error {
		service := apptui.NewService(func() (apptui.Backend, error) {
			return detectBackend()
		})

		return service.Run(ctx)
	}
)

func runBareHetki(cmd *cobra.Command, _ []string) error {
	if !isTerminal(os.Stdin.Fd()) || !isTerminal(os.Stdout.Fd()) {
		return cmd.Help()
	}
	return openTUI(cmd.Context())
}
