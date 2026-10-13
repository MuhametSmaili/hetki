package state

import (
	"strings"

	"github.com/MSmaili/hetki/internal/tmuxlayout"
)

type windowKey struct {
	Name string
	Path string
}

func compareWindows(diff *Diff, desired, actual *State) {
	common := CommonSessions(desired, actual)

	for _, sessionName := range common {
		desiredSession := desired.Sessions[sessionName]
		actualSession := actual.Sessions[sessionName]

		windowDiff := compareSessionWindows(desiredSession.Windows, actualSession.Windows)
		if !windowDiff.IsEmpty() {
			diff.Windows[sessionName] = windowDiff
		}
	}
}

func compareSessionWindows(desired, actual []*Window) ItemDiff[Window] {
	matched := make([]bool, len(actual))
	windowDiff := ItemDiff[Window]{
		Missing:    make([]Window, 0, len(desired)),
		Extra:      make([]Window, 0, len(actual)),
		Mismatched: make([]Mismatch[Window], 0),
	}

	for _, desiredWindow := range desired {
		candidate := -1
		for i, actualWindow := range actual {
			if matched[i] || keyForWindow(desiredWindow) != keyForWindow(actualWindow) {
				continue
			}
			if candidate < 0 {
				candidate = i
			}
			if windowsMatch(desiredWindow, actualWindow) {
				candidate = i
				break
			}
		}
		if candidate < 0 {
			windowDiff.Missing = append(windowDiff.Missing, *desiredWindow)
			continue
		}

		matched[candidate] = true
		actualWindow := actual[candidate]
		if !windowsMatch(desiredWindow, actualWindow) {
			windowDiff.Mismatched = append(windowDiff.Mismatched, Mismatch[Window]{
				Desired: *desiredWindow,
				Actual:  *actualWindow,
			})
		}
	}

	for i, actualWindow := range actual {
		if !matched[i] {
			windowDiff.Extra = append(windowDiff.Extra, *actualWindow)
		}
	}
	return windowDiff
}

func windowsMatch(desired, actual *Window) bool {
	if !layoutMatches(desired.Layout, actual.Layout) {
		return false
	}

	if len(desired.Panes) != len(actual.Panes) {
		return false
	}

	// Panes are matched by stable positional order after window matching by name+path.
	for i := range desired.Panes {
		if !panesMatch(desired.Panes[i], actual.Panes[i]) {
			return false
		}
	}

	return true
}

// panesMatch compares steady-state pane properties only. Command is a
// creation-time effect (D1): a running command never makes a pane look
// drifted, so start/force never re-sends it.
func panesMatch(desired, actual *Pane) bool {
	return desired.Path == actual.Path && desired.Zoom == actual.Zoom
}

func layoutMatches(desired, actual string) bool {
	if desired == "" || desired == actual {
		return true
	}

	// Best effort: manifests commonly use preset names like "tiled", while tmux
	// reports the live layout as a serialized layout string that will not match
	// the preset name directly.
	if isNamedLayout(desired) {
		return true
	}

	return tmuxlayout.SameGeometry(desired, actual)
}

func isNamedLayout(layout string) bool {
	switch strings.TrimSpace(layout) {
	case "even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled":
		return true
	default:
		return false
	}
}

func keyForWindow(w *Window) windowKey {
	return windowKey{Name: w.Name, Path: w.Path}
}
