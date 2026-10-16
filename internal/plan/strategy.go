package plan

import "sort"

type Strategy interface {
	Plan(diff Diff) *Plan
}

type MergeStrategy struct{}

func (s *MergeStrategy) Plan(diff Diff) *Plan {
	plan := &Plan{Actions: []Action{}}
	createMissing(plan, diff)
	return plan
}

type ForceStrategy struct{}

func (s *ForceStrategy) Plan(diff Diff) *Plan {
	plan := &Plan{Actions: []Action{}}
	// Keep sessions alive until replacement windows and their follow-ups exist.
	createMissing(plan, diff)
	recreateMismatched(plan, diff)
	killExtraWindows(plan, diff)
	return plan
}

func killExtraWindows(plan *Plan, diff Diff) {
	for _, sessionName := range sortedWindowDiffSessions(diff.Windows) {
		windowDiff := diff.Windows[sessionName]
		for _, window := range windowDiff.Extra {
			plan.Actions = append(plan.Actions, KillWindowAction{
				Session:  sessionName,
				Window:   window.Name,
				WindowID: window.ID,
			})
		}
	}
}

func recreateMismatched(plan *Plan, diff Diff) {
	for _, sessionName := range sortedWindowDiffSessions(diff.Windows) {
		windowDiff := diff.Windows[sessionName]
		for _, mismatch := range windowDiff.Mismatched {
			createWindow(plan, sessionName, mismatch.Desired)
			plan.Actions = append(plan.Actions, KillWindowAction{
				Session:  sessionName,
				Window:   mismatch.Actual.Name,
				WindowID: mismatch.Actual.ID,
			})
		}
	}
}

func createMissing(plan *Plan, diff Diff) {
	for _, session := range diff.Sessions.Missing {
		createSession(plan, session)
	}

	for _, sessionName := range sortedWindowDiffSessions(diff.Windows) {
		windowDiff := diff.Windows[sessionName]
		for _, window := range windowDiff.Missing {
			createWindow(plan, sessionName, window)
		}
	}
}

func createSession(plan *Plan, session Session) {
	if len(session.Windows) == 0 {
		return
	}

	firstWindow := session.Windows[0]
	plan.Actions = append(plan.Actions, CreateSessionAction{
		Name:       session.Name,
		WindowName: firstWindow.Name,
		Path:       firstWindow.Path,
	})
	addPanesAndCommands(plan, session.Name, firstWindow)

	for _, window := range session.Windows[1:] {
		createWindow(plan, session.Name, window)
	}
}

func createWindow(plan *Plan, sessionName string, window Window) {
	plan.Actions = append(plan.Actions, CreateWindowAction{
		Session: sessionName,
		Name:    window.Name,
		Path:    window.Path,
	})
	addPanesAndCommands(plan, sessionName, window)
}

func addPanesAndCommands(plan *Plan, sessionName string, window Window) {
	if len(window.Panes) > 1 {
		for _, pane := range window.Panes[1:] {
			plan.Actions = append(plan.Actions, SplitPaneAction{
				Session: sessionName,
				Window:  window.Name,
				Path:    pane.Path,
			})
		}
	}

	if window.Layout != "" {
		plan.Actions = append(plan.Actions, SelectLayoutAction{
			Session: sessionName,
			Window:  window.Name,
			Layout:  window.Layout,
		})
	}

	for i, pane := range window.Panes {
		if pane.Command != "" {
			plan.Actions = append(plan.Actions, SendKeysAction{
				Session: sessionName,
				Window:  window.Name,
				Pane:    i,
				Command: pane.Command,
			})
		}
		if pane.Zoom {
			plan.Actions = append(plan.Actions, ZoomPaneAction{
				Session: sessionName,
				Window:  window.Name,
				Pane:    i,
			})
		}
	}
}

func sortedWindowDiffSessions[V any](diff map[string]ItemDiff[V]) []string {
	names := make([]string, 0, len(diff))
	for name := range diff {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
