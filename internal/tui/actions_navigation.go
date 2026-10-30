package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

func keyModeFor(mode uiMode) KeyMode {
	switch mode {
	case modeFilter:
		return KeyModeFilter
	case modeInput:
		return KeyModePrompt
	case modeConfirm:
		return KeyModeConfirm
	case modeMenu:
		return KeyModeMenu
	default:
		return KeyModeNormal
	}
}

func handleQuit(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

func handleMoveUp(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeMenu:
		m.menu.Cursor = max(0, m.menu.Cursor-1)
	case modeBrowse, modeFilter:
		m.items.Move(-1)
	default:
		return m.actionUnavailable(ActionMoveUp)
	}
	return m.reflow(), nil
}

func handleMoveDown(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeMenu:
		m.menu.Cursor = min(len(m.menu.Entries)-1, m.menu.Cursor+1)
	case modeBrowse, modeFilter:
		m.items.Move(1)
	default:
		return m.actionUnavailable(ActionMoveDown)
	}
	return m.reflow(), nil
}

func handleMoveTop(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	if !m.isListMode() {
		return m.actionUnavailable(ActionMoveTop)
	}
	m.items.Top()
	return m.reflow(), nil
}

func handleMoveBottom(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	if !m.isListMode() {
		return m.actionUnavailable(ActionMoveBottom)
	}
	m.items.Bottom()
	return m.reflow(), nil
}

func handlePageUp(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	if !m.isListMode() {
		return m.actionUnavailable(ActionPageUp)
	}
	m.items.Page(-1)
	return m.reflow(), nil
}

func handlePageDown(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	if !m.isListMode() {
		return m.actionUnavailable(ActionPageDown)
	}
	m.items.Page(1)
	return m.reflow(), nil
}

func handleFilter(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	m.clearOverlayState()
	m.mode = modeFilter
	return m.reflow(), nil
}

func handleNextMatch(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	if !m.isListMode() {
		return m.actionUnavailable(ActionNextMatch)
	}
	m.items.JumpMatch(true)
	return m.reflow(), nil
}

func handlePrevMatch(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	if !m.isListMode() {
		return m.actionUnavailable(ActionPrevMatch)
	}
	m.items.JumpMatch(false)
	return m.reflow(), nil
}

func handleClearFilter(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	if !m.isListMode() {
		return m.actionUnavailable(ActionClearFilter)
	}
	m.setQuery("")
	if m.mode == modeFilter {
		m.mode = modeBrowse
	}
	return m.reflow(), nil
}

func handleExpand(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	m, ok := m.prepareListChange(ActionExpand)
	if !ok {
		return m, nil
	}
	m.items.ToggleSelected(true)
	return m.reflow(), nil
}

func handleCollapse(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	m, ok := m.prepareListChange(ActionCollapse)
	if !ok {
		return m, nil
	}
	m.items.ToggleSelected(false)
	return m.reflow(), nil
}

func handleExpandAll(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	m, ok := m.prepareListChange(ActionExpandAll)
	if !ok {
		return m, nil
	}
	m.items.ExpandAll()
	return m.reflow(), nil
}

func handleCollapseAll(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	m, ok := m.prepareListChange(ActionCollapseAll)
	if !ok {
		return m, nil
	}
	m.items.CollapseAll()
	return m.reflow(), nil
}

func (m model) prepareListChange(action ActionID) (model, bool) {
	if !m.isListMode() {
		m.err = fmt.Errorf("action %q is unavailable in %s mode", action, keyModeFor(m.mode))
		return m, false
	}
	return m, true
}

func (m model) isListMode() bool {
	return m.mode == modeBrowse || m.mode == modeFilter
}

func (m model) actionUnavailable(action ActionID) (tea.Model, tea.Cmd) {
	m.err = fmt.Errorf("action %q is unavailable in %s mode", action, keyModeFor(m.mode))
	return m, nil
}
