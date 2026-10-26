package tui

import (
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// WindowPane pairs snapshot presentation with an occurrence-checked status reference.
// An empty Ref means this pane has no matching status evidence.
type WindowPane struct {
	PaneID  string
	Ref     status.PaneRef
	Active  bool
	Details list.Details
}

type windowRepresentative struct {
	ref    status.PaneRef
	paneID string
}

// representativePane chooses the pane a tree window row shows and opens: the
// highest-priority pane with any status, keeping the previous choice on ties.
// Without any status it is the active pane.
func representativePane(
	panes []WindowPane,
	previous status.PaneRef,
	values status.Snapshot,
	now time.Time,
) int {
	selected, active := -1, -1

	for i, pane := range panes {
		if pane.Active {
			active = i
		}

		if pane.Ref != (status.PaneRef{}) && pane.Ref == previous {
			selected = i
		}
	}

	if selected < 0 {
		selected = active
	}

	priority := 0
	if selected >= 0 {
		priority = statusPriority(displayedStatus(values.Lookup(panes[selected].Ref), now))
	}

	for i, pane := range panes {
		candidate := statusPriority(displayedStatus(values.Lookup(pane.Ref), now))
		if candidate > priority {
			selected, priority = i, candidate
		}
	}

	if priority == 0 {
		return active
	}

	return selected
}

func (m *model) applyStatusPresentation(now time.Time) {
	m.syncWindowStatus(now)

	m.applyScope(m.items.Query(), now)
}

func (m *model) syncWindowStatus(now time.Time) {
	p := &m.paneStatus
	p.windowChangeDue = p.nextWindowStatusChange(now)
	if len(p.windows) == 0 {
		// Retain the last tree choices across a flat-view detour.
		m.items.SetDetails(nil)
		return
	}

	selected := make(map[list.ItemID]windowRepresentative, len(p.windows))
	details := make(map[list.ItemID]list.Details, len(p.windows))

	for id, panes := range p.windows {
		i := representativePane(panes, p.representatives[id].ref, p.values, now)
		if i >= 0 {
			selected[id] = windowRepresentative{ref: panes[i].Ref, paneID: panes[i].PaneID}
			details[id] = panes[i].Details
		}
	}

	p.representatives = selected
	m.items.SetDetails(details)
}

func (p paneStatusState) nextWindowStatusChange(now time.Time) time.Time {
	var due time.Time
	for _, panes := range p.windows {
		for _, pane := range panes {
			if change := nextStatusChange(p.values.Lookup(pane.Ref), now); !change.IsZero() {
				due = earlierStatusTime(due, change)
			}
		}
	}
	return due
}

func (p paneStatusState) displayedPaneID(id list.ItemID) string {
	if len(p.windows[id]) == 0 {
		return ""
	}
	return p.representatives[id].paneID
}

func (p paneStatusState) rowValue(id list.ItemID, now time.Time) status.Value {
	if _, ok := p.windows[id]; ok {
		return displayedStatus(p.values.Lookup(p.representatives[id].ref), now)
	}

	return p.options.Bindings.ValueAt(id, p.values, now)
}
