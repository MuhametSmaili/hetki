package converter

import (
	"sort"

	"github.com/MuhametSmaili/hetki/internal/plan"
	"github.com/MuhametSmaili/hetki/internal/state"
)

func StateDiffToPlanDiff(sd state.Diff, desired, actual *state.State) plan.Diff {
	pd := plan.Diff{
		Windows: make(map[string]plan.ItemDiff[plan.Window]),
	}

	pd.Sessions.Missing = convertMissingSessions(sd.Sessions.Missing, desired)
	pd.Sessions.Extra = convertExtraSessions(sd.Sessions.Extra, actual)

	for _, sessionName := range sortedWindowDiffSessions(sd.Windows) {
		wd := sd.Windows[sessionName]
		pd.Windows[sessionName] = convertWindowDiff(wd)
	}

	return pd
}

func convertMissingSessions(names []string, desired *state.State) []plan.Session {
	sessions := make([]plan.Session, 0, len(names))
	for _, name := range names {
		session := desired.Sessions[name]
		ps := plan.Session{Name: name}
		for _, w := range session.Windows {
			ps.Windows = append(ps.Windows, StateWindowToPlan(w))
		}
		sessions = append(sessions, ps)
	}
	return sessions
}

func convertExtraSessions(names []string, actual *state.State) []plan.Session {
	sessions := make([]plan.Session, 0, len(names))
	for _, name := range names {
		sessions = append(sessions, plan.Session{ID: actual.Sessions[name].ID, Name: name})
	}
	return sessions
}

func convertWindowDiff(wd state.ItemDiff[state.Window]) plan.ItemDiff[plan.Window] {
	pwd := plan.ItemDiff[plan.Window]{}

	for _, w := range wd.Missing {
		pwd.Missing = append(pwd.Missing, StateWindowToPlan(&w))
	}
	for _, w := range wd.Extra {
		pwd.Extra = append(pwd.Extra, StateWindowToPlan(&w))
	}
	for _, m := range wd.Mismatched {
		pwd.Mismatched = append(pwd.Mismatched, plan.Mismatch[plan.Window]{
			Desired: StateWindowToPlan(&m.Desired),
			Actual:  StateWindowToPlan(&m.Actual),
		})
	}
	return pwd
}

func StateWindowToPlan(w *state.Window) plan.Window {
	pw := plan.Window{ID: w.ID, Name: w.Name, Path: w.Path, Layout: w.Layout}
	for _, p := range w.Panes {
		pw.Panes = append(pw.Panes, plan.Pane{Path: p.Path, Command: p.Command, Zoom: p.Zoom})
	}
	return pw
}

func sortedWindowDiffSessions[V any](diff map[string]V) []string {
	names := make([]string, 0, len(diff))
	for name := range diff {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
