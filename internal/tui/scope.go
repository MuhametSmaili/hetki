package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type scopeState struct {
	mode  Scope
	views [2]scopeView
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
	if m.scope.mode == ScopeTracked {
		included = m.trackedItems(now)
	}
	m.items.SetFilter(query, included)
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
	if !m.isListMode() {
		return m.actionUnavailable(ActionToggleScope)
	}
	current, next := 0, 1
	if m.scope.mode == ScopeTracked {
		current, next = 1, 0
	}
	selected, _ := m.items.Selected()
	m.scope.views[current] = scopeView{selected: selected.Item.ID, expanded: m.items.Expansion()}
	if m.scope.mode == ScopeTracked {
		m.scope.mode = ScopeAll
	} else {
		m.scope.mode = ScopeTracked
	}
	m.applyScope(m.items.Query(), time.Now())
	view := m.scope.views[next]
	m.items.RestoreExpansion(view.expanded)
	if !m.items.Select(view.selected) {
		m.items.Select(selected.Item.ID)
	}
	return m.reflow(), nil
}
