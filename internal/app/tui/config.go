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
			warnings = append(warnings, fmt.Sprintf("ui.view: allowed %s or %s; using default %s", ui.ViewFlat, ui.ViewTree, options.Initial.View))
		}
	}
	if preferences.UI.Scope != nil {
		scope := ui.Scope(*preferences.UI.Scope)
		if scope.Validate() == nil {
			options.Initial.Scope = scope
		} else {
			warnings = append(warnings, fmt.Sprintf("ui.scope: allowed %s or %s; using default %s", ui.ScopeAll, ui.ScopeTracked, options.Initial.Scope))
		}
	}
	if preferences.UI.Preview != nil {
		options.Initial.Preview = *preferences.UI.Preview
	}
	if preferences.UI.PreviewWidth != nil {
		width := *preferences.UI.PreviewWidth
		if width >= 1 && width <= 99 {
			options.PreviewWidth = width
		} else {
			warnings = append(warnings, fmt.Sprintf("ui.preview_width: allowed 1–99; using default %d", options.PreviewWidth))
		}
	}
	return options, strings.Join(warnings, "; ")
}
