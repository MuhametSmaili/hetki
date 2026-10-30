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
		{name: "stay open", args: []string{"--stay-open"}, want: apptui.Overrides{StayOpen: true}},
		{name: "stay open explicitly enabled", args: []string{"--stay-open=true"}, want: apptui.Overrides{StayOpen: true}},
		{name: "stay open explicitly disabled", args: []string{"--stay-open=false"}, want: apptui.Overrides{}},
		{name: "last stay open wins", args: []string{"--stay-open", "--stay-open=false"}, want: apptui.Overrides{}},
		{name: "rows", args: []string{"--height", "20"}, want: apptui.Overrides{Height: new(ui.Height{Value: 20})}},
		{name: "percentage", args: []string{"--height=40%"}, want: apptui.Overrides{Height: new(ui.Height{Value: 40, Percent: true})}},
		{name: "explicit fullscreen default", args: []string{"--height=100%"}, want: apptui.Overrides{Height: new(ui.Height{Value: 100, Percent: true})}},
		{name: "100 rows is inline", args: []string{"--height=100"}, want: apptui.Overrides{Height: new(ui.Height{Value: 100})}},
		{name: "border only", args: []string{"--border", "none"}, want: apptui.Overrides{Border: new(ui.BorderNone)}},
		{name: "explicit rounded", args: []string{"--border=rounded"}, want: apptui.Overrides{Border: new(ui.BorderRounded)}},
		{name: "preview enabled", args: []string{"--preview"}, want: apptui.Overrides{Preview: new(true)}},
		{name: "preview explicitly enabled", args: []string{"--preview=true"}, want: apptui.Overrides{Preview: new(true)}},
		{name: "preview explicitly disabled", args: []string{"--preview=false"}, want: apptui.Overrides{Preview: new(false)}},
		{
			name: "combined",
			args: []string{"--height=12", "--border=none", "--preview", "--scope=tracked", "--view=tree", "--preview-width=45", "--stay-open"},
			want: apptui.Overrides{
				StayOpen:     true,
				Height:       new(ui.Height{Value: 12}),
				Border:       new(ui.BorderNone),
				Preview:      new(true),
				Scope:        new(ui.ScopeTracked),
				View:         new(ui.ViewTree),
				PreviewWidth: new(45),
			},
		},
		{name: "last preview flag wins", args: []string{"--preview", "--preview=false"}, want: apptui.Overrides{Preview: new(false)}},
		{name: "last preview flag enables", args: []string{"--preview=false", "--preview"}, want: apptui.Overrides{Preview: new(true)}},
		{name: "tracked scope", args: []string{"--scope", "tracked"}, want: apptui.Overrides{Scope: new(ui.ScopeTracked)}},
		{name: "explicit all scope", args: []string{"--scope=all"}, want: apptui.Overrides{Scope: new(ui.ScopeAll)}},
		{name: "scope with preview", args: []string{"--scope=tracked", "--preview=false"}, want: apptui.Overrides{Scope: new(ui.ScopeTracked), Preview: new(false)}},
		{name: "last scope wins", args: []string{"--scope=tracked", "--scope=all"}, want: apptui.Overrides{Scope: new(ui.ScopeAll)}},
		{name: "tree view", args: []string{"--view", "tree"}, want: apptui.Overrides{View: new(ui.ViewTree)}},
		{name: "explicit flat view", args: []string{"--view=flat"}, want: apptui.Overrides{View: new(ui.ViewFlat)}},
		{name: "last view wins", args: []string{"--view=tree", "--view=flat"}, want: apptui.Overrides{View: new(ui.ViewFlat)}},
		{name: "minimum preview width", args: []string{"--preview-width=1"}, want: apptui.Overrides{PreviewWidth: new(1)}},
		{name: "maximum preview width", args: []string{"--preview-width=99"}, want: apptui.Overrides{PreviewWidth: new(99)}},
		{name: "custom preview width", args: []string{"--preview-width", "45"}, want: apptui.Overrides{PreviewWidth: new(45)}},
		{name: "last preview width wins", args: []string{"--preview-width=45", "--preview-width=65"}, want: apptui.Overrides{PreviewWidth: new(65)}},
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
		{"--preview=invalid"}, {"--preview="},
		{"--stay-open=invalid"}, {"--stay-open="},
		{"--scope="}, {"--scope=other"}, {"--scope=working"}, {"--scope=Tracked"}, {"--scope"},
		{"--filter=tracked"},
		{"--view="}, {"--view=other"}, {"--view=Tree"}, {"--view"},
		{"--query=editor"},
		{"--preview-width=0"}, {"--preview-width=-1"}, {"--preview-width=100"},
		{"--preview-width=45.5"}, {"--preview-width=45%"}, {"--preview-width=other"},
		{"--preview-width=18446744073709551616"}, {"--preview-width="}, {"--preview-width"},
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

func TestPickerFlagValueCompletion(t *testing.T) {
	for _, tt := range []struct {
		flag, want string
	}{
		{flag: "scope", want: "all\ntracked\n:4\n"},
		{flag: "view", want: "flat\ntree\n:4\n"},
		{flag: "preview-width", want: ":4\n"},
	} {
		t.Run(tt.flag, func(t *testing.T) {
			command := &cobra.Command{Use: "hetki", RunE: runBareHetki}
			addPickerFlags(command)
			command.SetArgs([]string{"__complete", "--" + tt.flag, ""})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&bytes.Buffer{})

			err := command.Execute()

			require.NoError(t, err)
			require.Equal(t, tt.want, output.String())
		})
	}
}

func TestPickerFlagsAreLocalAndHelpDoesNotOpenPicker(t *testing.T) {
	for _, subcommand := range rootCmd.Commands() {
		for _, name := range []string{"height", "fullscreen", "border", "preview", "filter", "scope", "view", "preview-width", "query", "stay-open"} {
			require.Nil(t, subcommand.InheritedFlags().Lookup(name))
		}
	}
	command := &cobra.Command{Use: "hetki", RunE: runBareHetki}
	addPickerFlags(command)
	command.SetArgs([]string{"--height=12", "--preview", "--scope=tracked", "--view=tree", "--preview-width=45", "--help"})
	var output bytes.Buffer
	command.SetOut(&output)

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "--height")
	require.Contains(t, output.String(), "--stay-open")
	require.Contains(t, output.String(), "--border")
	require.Contains(t, output.String(), "--preview")
	require.Contains(t, output.String(), "--preview=false")
	require.Contains(t, output.String(), "--scope")
	require.Contains(t, output.String(), "all or tracked")
	require.Contains(t, output.String(), "--view")
	require.Contains(t, output.String(), "flat or tree")
	require.Contains(t, output.String(), "--preview-width")
	require.Contains(t, output.String(), "1-99")
	require.NotContains(t, output.String(), "--query")
	require.NotContains(t, output.String(), "--filter")
	require.NotContains(t, output.String(), "--fullscreen")
	require.Nil(t, rootCmd.Flags().Lookup("fullscreen"))
}
