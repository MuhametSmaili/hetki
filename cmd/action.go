package cmd

import (
	"fmt"
	"os"

	"github.com/MuhametSmaili/hetki/internal/app/actions"
	apptui "github.com/MuhametSmaili/hetki/internal/app/tui"
	"github.com/spf13/cobra"
)

var openActions = apptui.OpenActions

func init() {
	rootCmd.AddCommand(newActionCommand())
}

func newActionCommand() *cobra.Command {
	var pane, session, window, path string
	command := &cobra.Command{
		Use: "action", Short: "Open user-defined actions for a tmux pane, window or session",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			subject := actionSubject(pane, window, session)
			if err := subject.Validate(); err != nil {
				return err
			}
			overrides, err := pickerLayoutOverrides(cmd.Flags())
			if err != nil {
				return err
			}
			if err := overrides.Validate(); err != nil {
				return err
			}
			if !isTerminal(os.Stdin.Fd()) || !isTerminal(os.Stdout.Fd()) {
				return fmt.Errorf("action requires an interactive terminal")
			}
			return openActions(cmd.Context(), subject, path, overrides)
		},
	}
	flags := command.Flags()
	flags.StringVar(&pane, "pane-id", "", "Exact pane ID, for example %42")
	flags.StringVar(&session, "session", "", "Exact session ID, for example $3")
	flags.StringVar(&window, "window", "", "Exact window ID, for example @8")
	flags.StringVar(&path, "file", "", "Action recipes (default: $XDG_CONFIG_HOME/hetki/actions.yaml)")
	flags.String("height", "", "Picker height in rows or percent")
	flags.String("border", "", "Outer picker border: rounded or none")
	command.MarkFlagsOneRequired("pane-id", "session", "window")
	command.MarkFlagsMutuallyExclusive("pane-id", "session", "window")
	return command
}

func actionSubject(pane, window, session string) actions.Subject {
	switch {
	case pane != "":
		return actions.Subject{Kind: "pane", PaneID: pane}
	case window != "":
		return actions.Subject{Kind: "window", WindowID: window}
	default:
		return actions.Subject{Kind: "session", SessionID: session}
	}
}
