package tui

import (
	"image/color"

	"github.com/MuhametSmaili/hetki/internal/config"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

func resolveTheme(preferences *config.Theme, palette *ui.Palette) []string {
	if preferences == nil {
		return nil
	}

	var warnings []string
	warning := resolveThemeColor("accent", preferences.Accent, &palette.Accent)
	if warning != "" {
		warnings = append(warnings, warning)
	} else if preferences.Accent != nil {
		palette.Border = palette.Accent
		palette.Working = palette.Accent
	}

	for _, field := range []struct {
		name   string
		value  *string
		target *color.Color
	}{
		{"text", preferences.Text, &palette.Text},
		{"context", preferences.Context, &palette.Context},
		{"command", preferences.Command, &palette.Command},
		{"muted", preferences.Muted, &palette.Muted},
		{"hint", preferences.Hint, &palette.Hint},
		{"secondary", preferences.Secondary, &palette.Secondary},
		{"emphasis", preferences.Emphasis, &palette.Emphasis},
		{"active", preferences.Active, &palette.Active},
		{"border", preferences.Border, &palette.Border},
		{"divider", preferences.Divider, &palette.Divider},
		{"working", preferences.Working, &palette.Working},
		{"blocked", preferences.Blocked, &palette.Blocked},
		{"idle", preferences.Idle, &palette.Idle},
		{"success", preferences.Success, &palette.Success},
		{"error", preferences.Error, &palette.Error},
		{"selection_fg", preferences.SelectionFG, &palette.SelectionFG},
		{"selection_bg", preferences.SelectionBG, &palette.SelectionBG},
	} {
		if warning := resolveThemeColor(field.name, field.value, field.target); warning != "" {
			warnings = append(warnings, warning)
		}
	}

	return warnings
}

func resolveThemeColor(name string, value *string, target *color.Color) string {
	if value == nil {
		return ""
	}

	parsed, err := ui.ParseHexColor(*value)
	if err != nil {
		return "theme." + name + ": use #RRGGBB; invalid preference ignored"
	}

	*target = parsed
	return ""
}
