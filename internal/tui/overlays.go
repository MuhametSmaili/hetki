package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type inputState struct {
	Title        string
	Prompt       string
	Request      ActionRequest
	Value        string
	SubmitStatus string
	ReturnMode   uiMode
}

type confirmState struct {
	Title        string
	Body         string
	Request      ActionRequest
	SubmitStatus string
	ReturnMode   uiMode
}

type menuState struct {
	Title      string
	ItemID     list.ItemID
	Entries    []MenuEntry
	Cursor     int
	ReturnMode uiMode
}

func (m *model) openOverlay(result ActionResult, pending *ActionRequest) {
	if pending == nil {
		m.err = fmt.Errorf("overlay action has no pending request")
		return
	}
	returnMode := m.currentReturnMode()
	switch {
	case result.Menu != nil:
		menu := result.Menu
		if err := validateItemMenu(*menu); err != nil {
			m.err = err
			return
		}
		title := strings.TrimSpace(menu.Title)
		if title == "" {
			title = "ACTIONS"
		}
		m.mode = modeMenu
		m.menu = menuState{
			Title: title, ItemID: pending.ItemID,
			Entries: append([]MenuEntry(nil), menu.Entries...), ReturnMode: returnMode,
		}
	case result.Input != nil:
		prompt := result.Input
		m.mode = modeInput
		m.input = inputState{
			Title: prompt.Title, Prompt: prompt.Prompt, Request: *pending,
			Value: prompt.InitialValue, SubmitStatus: prompt.SubmitStatus, ReturnMode: returnMode,
		}
	case result.Confirmation != nil:
		confirmation := result.Confirmation
		m.mode = modeConfirm
		m.confirm = confirmState{
			Title: confirmation.Title, Body: confirmation.Body, Request: *pending,
			SubmitStatus: confirmation.SubmitStatus, ReturnMode: returnMode,
		}
	}
}

func (m model) updateMenuMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if action, ok := m.keys.Action(KeyModeMenu, msg); ok {
		if index := m.menuEntryIndex(action); index >= 0 {
			return m.activateMenuEntry(index)
		}
		if requiresMenuEntry(action) {
			return m.actionUnavailable(action)
		}
		return m.dispatchAction(action, list.ItemID{})
	}
	return m, nil
}

func isMenuControl(action ActionID) bool {
	return action == ActionCancel || action == ActionConfirm || action == ActionMoveUp || action == ActionMoveDown
}

func (m model) menuEntryIndex(action ActionID) int {
	for i, entry := range m.menu.Entries {
		if entry.Action == action {
			return i
		}
	}
	return -1
}

func (m model) activateMenuEntry(index int) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.menu.Entries) {
		m.err = fmt.Errorf("menu selection is unavailable")
		return m, nil
	}
	entry := m.menu.Entries[index]
	itemID := m.menu.ItemID
	m.mode = primaryReturnMode(m.menu.ReturnMode)
	m.menu = menuState{}
	return m.dispatchAction(entry.Action, itemID)
}

func handleCancel(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		m.mode = modeBrowse
	case modeInput:
		m.mode = primaryReturnMode(m.input.ReturnMode)
		m.input = inputState{}
		m.err = nil
	case modeConfirm:
		m.mode = primaryReturnMode(m.confirm.ReturnMode)
		m.confirm = confirmState{}
	case modeMenu:
		m.mode = primaryReturnMode(m.menu.ReturnMode)
		m.menu = menuState{}
	default:
		return m.actionUnavailable(ActionCancel)
	}
	return m, nil
}

func handleConfirm(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		m.mode = modeBrowse
		return m.dispatchAction(ActionOpen, list.ItemID{})
	case modeInput:
		value := strings.TrimSpace(m.input.Value)
		if value == "" {
			m.err = errors.New(statusValueEmpty)
			return m, nil
		}
		request := m.input.Request
		request.Value = &value
		status := m.input.SubmitStatus
		if strings.TrimSpace(status) == "" {
			status = statusRunningAction
		}
		m.mode = primaryReturnMode(m.input.ReturnMode)
		m.input = inputState{}
		return m.startRequest(request, status)
	case modeConfirm:
		request := m.confirm.Request
		request.Confirmed = true
		status := m.confirm.SubmitStatus
		if strings.TrimSpace(status) == "" {
			status = statusRunningAction
		}
		m.mode = primaryReturnMode(m.confirm.ReturnMode)
		m.confirm = confirmState{}
		return m.startRequest(request, status)
	case modeMenu:
		return m.activateMenuEntry(m.menu.Cursor)
	default:
		return m.actionUnavailable(ActionConfirm)
	}
}

func (m model) currentReturnMode() uiMode {
	switch m.mode {
	case modeInput:
		return primaryReturnMode(m.input.ReturnMode)
	case modeConfirm:
		return primaryReturnMode(m.confirm.ReturnMode)
	case modeMenu:
		return primaryReturnMode(m.menu.ReturnMode)
	default:
		return primaryReturnMode(m.mode)
	}
}

func (m *model) clearOverlayState() {
	switch m.mode {
	case modeInput:
		m.input = inputState{}
	case modeConfirm:
		m.confirm = confirmState{}
	case modeMenu:
		m.menu = menuState{}
	}
}

func primaryReturnMode(mode uiMode) uiMode {
	if mode == modeFilter {
		return modeFilter
	}
	return modeBrowse
}

func validateItemMenu(menu ItemMenu) error {
	if len(menu.Entries) == 0 {
		return fmt.Errorf("selected item has no available actions")
	}
	actions := make(map[ActionID]struct{}, len(menu.Entries))
	for _, entry := range menu.Entries {
		if entry.Action == "" || strings.TrimSpace(entry.Label) == "" {
			return fmt.Errorf("menu entry has an invalid action or label")
		}
		if isMenuControl(entry.Action) {
			return fmt.Errorf("menu action %q is reserved for menu controls", entry.Action)
		}
		if _, exists := actions[entry.Action]; exists {
			return fmt.Errorf("menu action %q appears more than once", entry.Action)
		}
		actions[entry.Action] = struct{}{}
	}
	return nil
}
