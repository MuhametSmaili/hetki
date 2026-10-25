package cmd

import (
	"bytes"
	"context"
	"testing"

	apptui "github.com/MuhametSmaili/hetki/internal/app/tui"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestPickerFlagsPassOnlyExplicitOverrides(t *testing.T) {
	previousProbe, previousOpen := isTerminal, openTUI
	t.Cleanup(func() { isTerminal, openTUI = previousProbe, previousOpen })
	isTerminal = func(uintptr) bool { return true }
	for _, tt := range []struct {
		name string
		args []string
		want apptui.Overrides
	}{
		{name: "absent flags", want: apptui.Overrides{}},
		{name: "rows", args: []string{"--height", "20"}, want: apptui.Overrides{Height: new(ui.Height{Value: 20})}},
		{name: "percentage", args: []string{"--height=40%"}, want: apptui.Overrides{Height: new(ui.Height{Value: 40, Percent: true})}},
		{name: "explicit fullscreen default", args: []string{"--height=100%"}, want: apptui.Overrides{Height: new(ui.Height{Value: 100, Percent: true})}},
		{name: "100 rows is inline", args: []string{"--height=100"}, want: apptui.Overrides{Height: new(ui.Height{Value: 100})}},
		{name: "border only", args: []string{"--border", "none"}, want: apptui.Overrides{Border: new(ui.BorderNone)}},
		{name: "explicit rounded", args: []string{"--border=rounded"}, want: apptui.Overrides{Border: new(ui.BorderRounded)}},
		{name: "combined", args: []string{"--height=12", "--border=none"}, want: apptui.Overrides{Height: new(ui.Height{Value: 12}), Border: new(ui.BorderNone)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			command := &cobra.Command{Use: "hetki", RunE: runBareHetki}
			addPickerFlags(command)
			command.SetArgs(tt.args)
			command.SetContext(t.Context())
			calls := 0
			openTUI = func(ctx context.Context, overrides apptui.Overrides) error {
				calls++
				require.Same(t, command.Context(), ctx)
				require.Equal(t, tt.want, overrides)
				return nil
			}

			err := command.Execute()

			require.NoError(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestInvalidPickerFlagsFailWithoutOpeningTUI(t *testing.T) {
	previousProbe, previousOpen := isTerminal, openTUI
	t.Cleanup(func() { isTerminal, openTUI = previousProbe, previousOpen })
	isTerminal = func(uintptr) bool { return true }
	openTUI = func(context.Context, apptui.Overrides) error {
		t.Fatal("invalid flags opened picker")
		return nil
	}
	for _, args := range [][]string{
		{"--height=0"}, {"--height=-1"}, {"--height=0%"}, {"--height=101%"},
		{"--height=1.5%"}, {"--height="}, {"--border=other"},
		{"--fullscreen"}, {"--fullscreen=false"},
	} {
		t.Run(args[0], func(t *testing.T) {
			command := &cobra.Command{Use: "hetki", RunE: runBareHetki}
			addPickerFlags(command)
			command.SetArgs(args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})

			require.Error(t, command.Execute())
		})
	}
}

func TestPickerFlagsAreLocalAndHelpDoesNotOpenPicker(t *testing.T) {
	for _, subcommand := range rootCmd.Commands() {
		for _, name := range []string{"height", "fullscreen", "border"} {
			require.Nil(t, subcommand.InheritedFlags().Lookup(name))
		}
	}
	command := &cobra.Command{Use: "hetki", RunE: runBareHetki}
	addPickerFlags(command)
	command.SetArgs([]string{"--height=12", "--help"})
	var output bytes.Buffer
	command.SetOut(&output)

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "--height")
	require.Contains(t, output.String(), "--border")
	require.NotContains(t, output.String(), "--fullscreen")
	require.Nil(t, rootCmd.Flags().Lookup("fullscreen"))
}
