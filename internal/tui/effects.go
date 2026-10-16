package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type DispatchFunc func(ActionRequest) (ActionResult, error)

type actionResultMsg struct {
	result ActionResult
	err    error
}

func (m model) handleActionResult(msg actionResultMsg) (tea.Model, tea.Cmd) {
	result := msg.result
	pending, previousRows := m.pending, m.pendingRows
	resumeJump := m.mode == modeJump && pending != nil && pending.ActionID == ActionToggleProjection
	m.pending, m.pendingRows = nil, nil
	m.busy, m.status = false, ""
	if result.Snapshot != nil && m.mode == modeJump {
		m.cancelJump()
	}
	m.err = msg.err
	switch {
	case m.err != nil:
		return m, nil
	case result.Menu != nil || result.Input != nil || result.Confirmation != nil:
		m.openOverlay(result, pending)
		return m.reflow(), nil
	case !result.Navigation.IsZero():
		m.navigation = result.Navigation
		return m, tea.Quit
	case result.Snapshot == nil:
		return m, nil
	}

	if err := m.items.Replace(*result.Snapshot, result.SelectItemID); err != nil {
		m.err = err
		return m, nil
	}
	m.preview.invalidate()
	m.paneStatus.options.Bindings = nil
	if result.Status != nil {
		m.applyStatusUpdate(*result.Status)
	}
	m.paneStatus.bindingGeneration++
	m.applyScope(m.items.Query(), time.Now())
	if !result.SelectItemID.IsZero() {
		m.items.Select(result.SelectItemID)
	}
	if pending != nil && isDeleteAction(pending.ActionID) {
		m.selectAfterDelete(pending.ItemID, result.SelectItemID, previousRows)
	}
	if resumeJump {
		return handleJump(m.reflow(), list.ItemID{})
	}
	return m.reflow(), nil
}

func isDeleteAction(action ActionID) bool {
	return action == ActionDelete || action == ActionDeleteSession
}

func (m *model) selectAfterDelete(deletedID, preferred list.ItemID, previousRows []list.ItemID) {
	if !preferred.IsZero() {
		if m.items.Select(preferred) {
			return
		}
		if m.items.Query() != "" {
			m.setQuery("")
			if m.items.Select(preferred) {
				return
			}
		}
	}
	if m.items.Select(deletedID) {
		return
	}
	if m.items.SelectSurvivor(previousRows, deletedID) {
		return
	}
	if m.items.Query() != "" {
		m.setQuery("")
		if m.items.SelectSurvivor(previousRows, deletedID) {
			return
		}
	}
	rows := m.items.Rows()
	if len(rows) > 0 {
		position := max(0, slices.Index(previousRows, deletedID))
		m.items.Select(rows[min(position, len(rows)-1)].Item.ID)
	}
}

func requiresMenuEntry(action ActionID) bool {
	switch action {
	case ActionContextMenu, ActionCreateSession, ActionCreateWindow, ActionRename, ActionRenameSession,
		ActionDelete, ActionDeleteSession, ActionRefresh, ActionToggleProjection, ActionOpen:
		return true
	default:
		return false
	}
}

func handleContextMenu(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionContextMenu, itemID)
}

func handleCreateSession(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startAction(ActionCreateSession, itemID)
}

func handleCreateWindow(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionCreateWindow, itemID)
}

func handleRename(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionRename, itemID)
}

func handleRenameSession(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionRenameSession, itemID)
}

func handleDelete(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionDelete, itemID)
}

func handleDeleteSession(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionDeleteSession, itemID)
}

func handleRefresh(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startAction(ActionRefresh, itemID)
}

func handleToggleProjection(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	if itemID.IsZero() {
		if selected, ok := m.selectedRow(); ok {
			itemID = selected.Item.ID
		}
	}
	return m.startAction(ActionToggleProjection, itemID)
}

func handleLastSession(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	return m.startAction(ActionLastSession, list.ItemID{})
}

func handleOpen(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeJump:
		m.cancelJump()
	case modeFilter:
		if itemID.IsZero() {
			m.mode = modeBrowse
		}
	}
	return m.startItemAction(ActionOpen, itemID)
}

func (m model) startItemAction(action ActionID, itemID list.ItemID) (tea.Model, tea.Cmd) {
	if !itemID.IsZero() {
		return m.startAction(action, itemID)
	}
	return m.startSelectedRequest(action)
}

func (m model) startSelectedRequest(action ActionID) (tea.Model, tea.Cmd) {
	selected, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	return m.startAction(action, selected.Item.ID)
}

func (m model) startAction(action ActionID, itemID list.ItemID) (tea.Model, tea.Cmd) {
	status := statusRunningAction
	switch action {
	case ActionOpen, ActionLastSession:
		status = statusSwitching
	case ActionRefresh:
		status = statusRefreshing
	}
	return m.startRequest(ActionRequest{ActionID: action, ItemID: itemID}, status)
}

func (m model) startRequest(request ActionRequest, status string) (tea.Model, tea.Cmd) {
	m.busy = true
	m.pending = cloneRequest(request)
	m.pendingRows = nil
	if isDeleteAction(request.ActionID) && request.Confirmed {
		rows := m.items.Rows()
		m.pendingRows = make([]list.ItemID, len(rows))
		for i, row := range rows {
			m.pendingRows[i] = row.Item.ID
		}
	}
	m.status = status
	return m, runAction(m.dispatch, request)
}

func runAction(dispatch DispatchFunc, request ActionRequest) tea.Cmd {
	if dispatch == nil {
		return nil
	}
	return func() tea.Msg {
		result, err := dispatch(request)
		return actionResultMsg{result: result, err: err}
	}
}

func cloneRequest(request ActionRequest) *ActionRequest {
	cloned := request
	return &cloned
}
