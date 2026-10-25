package tui

import (
	"fmt"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/config"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

// Invalid strings fall back independently; warnings never include file values.
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
	return options, strings.Join(warnings, "; ")
}
