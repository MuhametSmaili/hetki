package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type scopeState struct {
	tracked bool
	views   [2]scopeView
}

type scopeView struct {
	selected list.ItemID
	expanded map[list.ItemID]bool
}

func (m *model) setQuery(query string) {
	m.applyScope(query, time.Now())
}

func (m *model) applyScope(query string, now time.Time) {
	var included map[list.ItemID]struct{}
	if m.scope.tracked {
		included = m.trackedItems(now)
	}
	if m.items.SetFilter(query, included) && m.mode == modeJump {
		m.cancelJump()
	}
}

func (m model) trackedItems(now time.Time) map[list.ItemID]struct{} {
	included := make(map[list.ItemID]struct{})
	for id := range m.paneStatus.options.Bindings {
		value := m.paneStatus.options.Bindings.ValueAt(id, m.paneStatus.values, now)
		if statusPriority(value) > 0 {
			included[id] = struct{}{}
		}
	}
	return included
}

func handleToggleScope(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	jumping := m.mode == modeJump
	if !m.isListMode() && !jumping {
		return m.actionUnavailable(ActionToggleScope)
	}
	current, next := 0, 1
	if m.scope.tracked {
		current, next = 1, 0
	}
	selected, _ := m.items.Selected()
	m.scope.views[current] = scopeView{selected: selected.Item.ID, expanded: m.items.Expansion()}
	m.scope.tracked = !m.scope.tracked
	m.applyScope(m.items.Query(), time.Now())
	view := m.scope.views[next]
	m.items.RestoreExpansion(view.expanded)
	if !m.items.Select(view.selected) {
		m.items.Select(selected.Item.ID)
	}
	m = m.reflow()
	if jumping {
		return handleJump(m, list.ItemID{})
	}
	return m, nil
}
