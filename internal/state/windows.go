package state

type windowKey struct {
	Name string
	Path string
}

type windowComparer struct {
	layoutsMatch func(string, string) bool
}

func (c windowComparer) compareWindows(diff *Diff, desired, actual *State) {
	common := CommonSessions(desired, actual)

	for _, sessionName := range common {
		desiredSession := desired.Sessions[sessionName]
		actualSession := actual.Sessions[sessionName]

		windowDiff := c.compareSessionWindows(desiredSession.Windows, actualSession.Windows)
		if !windowDiff.IsEmpty() {
			diff.Windows[sessionName] = windowDiff
		}
	}
}

func (c windowComparer) compareSessionWindows(desired, actual []*Window) ItemDiff[Window] {
	matched := make([]bool, len(actual))
	windowDiff := ItemDiff[Window]{
		Missing:    make([]Window, 0, len(desired)),
		Extra:      make([]Window, 0, len(actual)),
		Mismatched: make([]Mismatch[Window], 0),
	}

	for _, desiredWindow := range desired {
		candidate := c.findCandidate(desiredWindow, actual, matched)
		if candidate < 0 {
			windowDiff.Missing = append(windowDiff.Missing, *desiredWindow)
			continue
		}

		matched[candidate] = true
		actualWindow := actual[candidate]
		if !c.windowsMatch(desiredWindow, actualWindow) {
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

func (c windowComparer) findCandidate(desired *Window, actual []*Window, matched []bool) int {
	candidate := -1
	for i, window := range actual {
		if matched[i] || desired.Name != window.Name || !observedPathMatches(desired.Path, window.Path) {
			continue
		}
		if WindowsCorrespond(desired, window) && c.windowsMatch(desired, window) {
			return i
		}
		if candidate < 0 || c.preferCandidate(desired, window, actual[candidate]) {
			candidate = i
		}
	}
	return candidate
}

// Fully known matches are selected by findCandidate. Among remaining candidates,
// prefer matching properties, then a known window path; keep ties in input order.
func (c windowComparer) preferCandidate(desired, candidate, current *Window) bool {
	candidateMatches := c.windowsMatch(desired, candidate)
	currentMatches := c.windowsMatch(desired, current)
	if candidateMatches != currentMatches {
		return candidateMatches
	}

	return candidateMatches && candidate.Path != "" && current.Path == ""
}

func (c windowComparer) windowsMatch(desired, actual *Window) bool {
	if desired.Layout != "" && desired.Layout != actual.Layout {
		if c.layoutsMatch == nil || !c.layoutsMatch(desired.Layout, actual.Layout) {
			return false
		}
	}

	if desired.Name != actual.Name || !observedPathMatches(desired.Path, actual.Path) || len(desired.Panes) != len(actual.Panes) {
		return false
	}

	for i := range desired.Panes {
		if !observedPathMatches(desired.Panes[i].Path, actual.Panes[i].Path) {
			return false
		}

		if desired.Panes[i].Zoom != actual.Panes[i].Zoom {
			return false
		}
	}

	return true
}

// An empty observed path is unavailable, not evidence of a different directory.
func observedPathMatches(desired, observed string) bool {
	return observed == "" || desired == observed
}

// WindowsCorrespond compares names and resolved directories, pairing panes by
// position. Layout and zoom are mutable properties, not correspondence keys.
// Commands are creation-time effects, never evidence of identity or drift.
// Callers must supply canonical paths and an explicit first pane for each window.
func WindowsCorrespond(desired, actual *Window) bool {
	if keyForWindow(desired) != keyForWindow(actual) || len(desired.Panes) != len(actual.Panes) {
		return false
	}
	for i := range desired.Panes {
		if desired.Panes[i].Path != actual.Panes[i].Path {
			return false
		}
	}
	return true
}

func keyForWindow(w *Window) windowKey {
	return windowKey{Name: w.Name, Path: w.Path}
}
