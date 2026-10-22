package tui

import (
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// WindowPane pairs snapshot presentation with an occurrence-checked status reference.
// An empty Ref means this pane has no matching status evidence.
type WindowPane struct {
	Ref     status.PaneRef
	Active  bool
	Details list.Details
}

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

	idlePriority := statusPriority(status.Value{State: status.Idle})
	priority := idlePriority
	if selected >= 0 {
		current := displayedStatus(values.Lookup(panes[selected].Ref), now)
		priority = max(priority, statusPriority(current))
	}

	for i, pane := range panes {
		candidate := statusPriority(displayedStatus(values.Lookup(pane.Ref), now))
		if candidate > priority {
			selected, priority = i, candidate
		}
	}

	if priority == idlePriority {
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

	selected := make(map[list.ItemID]status.PaneRef, len(p.windows))
	details := make(map[list.ItemID]list.Details, len(p.windows))

	for id, panes := range p.windows {
		i := representativePane(panes, p.representatives[id], p.values, now)
		selected[id] = status.PaneRef{}
		if i >= 0 {
			selected[id] = panes[i].Ref
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

func (p paneStatusState) rowValue(id list.ItemID, now time.Time) status.Value {
	if _, ok := p.windows[id]; ok {
		return displayedStatus(p.values.Lookup(p.representatives[id]), now)
	}

	return p.options.Bindings.ValueAt(id, p.values, now)
}
