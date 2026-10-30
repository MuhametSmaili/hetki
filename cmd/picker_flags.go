package cmd

import (
	apptui "github.com/MuhametSmaili/hetki/internal/app/tui"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() {
	addPickerFlags(rootCmd)
}

func addPickerFlags(command *cobra.Command) {
	flags := command.Flags()
	flags.Bool("stay-open", false, "Keep the picker running after switching (requires tmux)")
	flags.String("height", "", "Picker height in rows or percent (100% selects fullscreen)")
	flags.String("border", "", "Outer picker border: rounded or none")
	flags.Bool("preview", false, "Show pane preview initially (use --preview=false to disable)")
	flags.String("scope", "", "Initial item scope: all or tracked (items with status)")
	flags.String("view", "", "Initial picker view: flat or tree")
	flags.Int("preview-width", 0, "Preview width as an integer percentage of content width (1-99)")
	cobra.CheckErr(command.RegisterFlagCompletionFunc("preview-width", cobra.NoFileCompletions))
	cobra.CheckErr(command.RegisterFlagCompletionFunc("scope", cobra.FixedCompletions(
		[]cobra.Completion{string(ui.ScopeAll), string(ui.ScopeTracked)}, cobra.ShellCompDirectiveNoFileComp,
	)))
	cobra.CheckErr(command.RegisterFlagCompletionFunc("view", cobra.FixedCompletions(
		[]cobra.Completion{string(ui.ViewFlat), string(ui.ViewTree)}, cobra.ShellCompDirectiveNoFileComp,
	)))
}

func pickerOverrides(command *cobra.Command) (apptui.Overrides, error) {
	flags := command.Flags()
	overrides, err := pickerLayoutOverrides(flags)
	if err != nil {
		return overrides, err
	}
	if flags.Changed("stay-open") {
		overrides.StayOpen, err = flags.GetBool("stay-open")
		if err != nil {
			return overrides, err
		}
	}

	if flags.Changed("preview") {
		preview, err := flags.GetBool("preview")
		if err != nil {
			return overrides, err
		}
		overrides.Preview = &preview
	}
	if flags.Changed("scope") {
		scope, err := flags.GetString("scope")
		if err != nil {
			return overrides, err
		}
		overrides.Scope = new(ui.Scope(scope))
	}
	if flags.Changed("view") {
		view, err := flags.GetString("view")
		if err != nil {
			return overrides, err
		}
		overrides.View = new(ui.ViewMode(view))
	}
	return overrides, overrides.Validate()
}

func pickerLayoutOverrides(flags *pflag.FlagSet) (apptui.Overrides, error) {
	var overrides apptui.Overrides
	if flags.Changed("height") {
		value, err := flags.GetString("height")
		if err != nil {
			return overrides, err
		}
		height, err := ui.ParseHeight(value)
		if err != nil {
			return overrides, err
		}
		overrides.Height = &height
	}
	if flags.Changed("border") {
		border, err := flags.GetString("border")
		if err != nil {
			return overrides, err
		}
		overrides.Border = new(ui.BorderMode(border))
	}
	if flags.Changed("preview-width") {
		width, err := flags.GetInt("preview-width")
		if err != nil {
			return overrides, err
		}
		overrides.PreviewWidth = &width
	}
	return overrides, nil
}
