package tmux

import (
	"strings"

	"github.com/MSmaili/hetki/internal/backend/tmux/internal/layout"
)

// LayoutPreset converts an observation to a readable, approximate preset.
// It does not query or modify tmux; missing observations remain omitted.
func (b *TmuxBackend) LayoutPreset(observed string) (string, error) {
	return layout.Preset(observed)
}

// LayoutsMatch compares ordinary split geometry, ignoring runtime pane IDs.
// Preset requests remain best effort; invalid fallback observations do not match.
func (b *TmuxBackend) LayoutsMatch(desired, actual string) bool {
	if desired == "" || desired == actual {
		return true
	}

	switch strings.TrimSpace(desired) {
	case "even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled":
		return true
	}

	return layout.SameGeometry(desired, actual)
}
