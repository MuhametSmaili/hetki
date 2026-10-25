package tui

import (
	"fmt"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/config"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

// Invalid choices fall back independently; warnings never include file values.
func resolveConfig(preferences config.Config) (ui.Options, string) {
	options := ui.DefaultOptions()
	var warnings []string
	if preferences.UI.View != nil {
		view := ui.ViewMode(*preferences.UI.View)
		if view.Validate() == nil {
			options.Initial.View = view
		} else {
			warnings = append(warnings, fmt.Sprintf("ui.view: allowed %s or %s; invalid preference ignored", ui.ViewFlat, ui.ViewTree))
		}
	}
	if preferences.UI.Scope != nil {
		scope := ui.Scope(*preferences.UI.Scope)
		if scope.Validate() == nil {
			options.Initial.Scope = scope
		} else {
			warnings = append(warnings, fmt.Sprintf("ui.scope: allowed %s or %s; invalid preference ignored", ui.ScopeAll, ui.ScopeTracked))
		}
	}
	if preferences.UI.Preview != nil {
		options.Initial.Preview = *preferences.UI.Preview
	}
	if preferences.UI.PreviewWidth != nil {
		width := *preferences.UI.PreviewWidth
		if ui.ValidatePreviewWidth(width) == nil {
			options.PreviewWidth = width
		} else {
			warnings = append(warnings, "ui.preview_width: allowed 1–99; invalid preference ignored")
		}
	}
	warnings = append(warnings, resolvePresentation(preferences.UI, &options)...)
	return options, strings.Join(warnings, "; ")
}

func resolvePresentation(preferences config.UI, options *ui.Options) []string {
	var warnings []string
	if preferences.Height != nil {
		height, err := ui.ParseHeight(*preferences.Height)
		if err == nil {
			options.Height = height
		} else {
			warnings = append(warnings, "ui.height: use positive rows or 1% to 100%; invalid preference ignored")
		}
	}
	if preferences.Border != nil {
		border := ui.BorderMode(*preferences.Border)
		if border.Validate() == nil {
			options.Border = border
		} else {
			warnings = append(warnings, "ui.border: allowed rounded or none; invalid preference ignored")
		}
	}
	return warnings
}
