package tui

import (
	"maps"
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
	m.pending, m.pendingRows = nil, nil
	m.busy, m.status = false, ""
	m.err = msg.err
	if result.Choice != nil {
		return m.applyChoiceResult(result, msg.err)
	}
	if m.err == nil && result.View != "" {
		m.err = result.View.Validate()
	}
	if m.err == nil && m.mode == modeInput && len(m.input.Fields) > 0 && pending != nil && pending.Values != nil {
		m.mode = primaryReturnMode(m.input.ReturnMode)
		m.input = inputState{}
	}
	switch {
	case m.err != nil:
		return m, nil
	case result.Menu != nil || result.Input != nil || result.Confirmation != nil:
		m.openOverlay(result, pending)
		return m.reflow(), nil
	case !result.Navigation.IsZero():
		m.navigation = result.Navigation
		m.quitting = true
		return m, tea.Quit
	case result.Snapshot == nil:
		return m, nil
	}

	return m.applyActionSnapshot(result, pending, previousRows)
}

func (m model) applyActionSnapshot(result ActionResult, pending *ActionRequest, previousRows []list.ItemID) (tea.Model, tea.Cmd) {
	preferred := result.SelectItemID
	if preferred.IsZero() {
		selected, _ := m.items.Selected()
		preferred = selected.Item.ID
	}

	if err := m.items.Replace(*result.Snapshot, preferred); err != nil {
		m.err = err
		return m, nil
	}
	if result.View == ViewTree && m.view != ViewTree {
		m.items.RestoreExpansion(nil)
	}
	if result.View != "" {
		m.view = result.View
	}
	m.preview.invalidate()
	m.paneStatus.options.Bindings = nil
	m.paneStatus.windows = nil
	if result.Status != nil {
		m.applyStatusUpdate(*result.Status)
	}
	m.paneStatus.bindingGeneration++
	m.applyStatusPresentation(time.Now())
	if !m.items.Select(preferred) {
		m.revealCreatedSession(result.SelectItemID, pending)
	}
	if pending != nil && isDeleteAction(pending.ActionID) {
		m.selectAfterDelete(pending.ItemID, result.SelectItemID, previousRows)
	}
	return m.reflow(), nil
}

func isDeleteAction(action ActionID) bool {
	return action == ActionDelete || action == ActionDeleteSession
}

func (m *model) revealCreatedSession(id list.ItemID, pending *ActionRequest) {
	if pending == nil || pending.ActionID != ActionCreateSession || id.IsZero() {
		return
	}

	if m.scope.mode == ScopeTracked {
		m.toggleScope()
		if m.items.Select(id) {
			return
		}
	}
	if m.items.Query() != "" {
		m.setQuery("")
		m.items.Select(id)
	}
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
		ActionDelete, ActionDeleteSession, ActionRefresh, ActionToggleProjection, ActionOpen, ActionUserActions:
		return true
	default:
		return false
	}
}

func handleContextMenu(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionContextMenu, itemID)
}

func handleCreateSession(m model, itemID list.ItemID) (tea.Model, tea.Cmd) {
	if itemID.IsZero() {
		if selected, ok := m.selectedRow(); ok {
			itemID = selected.Item.ID
		}
	}

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
	if m.mode == modeFilter && itemID.IsZero() {
		m.mode = modeBrowse
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
	default:
		// Other actions keep statusRunningAction.
	}

	request := ActionRequest{ActionID: action, ItemID: itemID}
	if action == ActionOpen {
		request.PaneID = m.paneStatus.displayedPaneID(itemID)
	}
	return m.startRequest(request, status)
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
	cloned.Values = maps.Clone(request.Values)
	return &cloned
}
