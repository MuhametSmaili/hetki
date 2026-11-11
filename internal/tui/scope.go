package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type scopeState struct {
	mode      Scope
	views     [2]scopeView
	applied   Scope
	valid     bool
	changeDue time.Time
}

type scopeView struct {
	selected list.ItemID
	expanded map[list.ItemID]bool
}

func (m *model) setQuery(query string) {
	if query != m.items.Query() {
		m.search.selection, m.search.confirm = nil, nil
	}
	m.applyScope(query, time.Now())
}

func (m *model) applyScope(query string, now time.Time) {
	if m.scope.valid && m.scope.applied == m.scope.mode && (m.scope.changeDue.IsZero() || now.Before(m.scope.changeDue)) {
		m.items.SetFilterQuery(query)
		return
	}
	var included map[list.ItemID]struct{}
	m.scope.changeDue = time.Time{}
	if m.scope.mode == ScopeTracked {
		included = m.trackedItems(now)
		for _, value := range m.paneStatus.values.All() {
			if change := nextStatusChange(value, now); !change.IsZero() {
				m.scope.changeDue = earlierStatusTime(m.scope.changeDue, change)
			}
		}
	}
	m.items.SetFilter(query, included)
	m.scope.valid, m.scope.applied = true, m.scope.mode
}

func (m model) trackedItems(now time.Time) map[list.ItemID]struct{} {
	included := make(map[list.ItemID]struct{})
	for id := range m.paneStatus.options.Bindings {
		if m.ctx.Err() != nil {
			return included
		}
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
	m.toggleScope()
	return m.reflow(), nil
}

func (m *model) toggleScope() {
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
	m.items.PreferSelection(view.selected, selected.Item.ID)
}
