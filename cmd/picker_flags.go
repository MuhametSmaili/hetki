package cmd

import (
	apptui "github.com/MuhametSmaili/hetki/internal/app/tui"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/spf13/cobra"
)

func init() {
	addPickerFlags(rootCmd)
}

func addPickerFlags(command *cobra.Command) {
	flags := command.Flags()
	flags.String("height", "", "Picker height in rows or percent (100% selects fullscreen)")
	flags.String("border", "", "Outer picker border: rounded or none")
}

func pickerOverrides(command *cobra.Command) (apptui.Overrides, error) {
	flags := command.Flags()
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
	return overrides, overrides.Validate()
}
