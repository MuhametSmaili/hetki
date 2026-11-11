package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type searchState struct {
	worker    *searchWorker
	requested list.SearchKey
	active    bool
	selection *snapshotSelection
	confirm   *searchConfirmation
}

type searchConfirmation struct {
	key    list.SearchKey
	action ActionID
}

type snapshotSelection struct {
	preferred list.ItemID
	created   list.ItemID
	pending   *ActionRequest
	previous  []list.ItemID
}

func (m *model) syncSearch() {
	if m.search.worker == nil {
		return
	}
	if m.quitting {
		m.replacement = nil
		m.search.confirm = nil
		m.search.worker.submit(nil)
		m.search.active = false
		return
	}
	if m.replacement != nil {
		return
	}
	request, pending := m.items.PendingSearch()
	if m.search.confirm != nil && (m.quitting || request.Key() != m.search.confirm.key) {
		m.search.confirm = nil
	}
	if m.quitting || !pending {
		if m.search.active {
			m.search.worker.submit(nil)
			m.search.active = false
		}
		return
	}
	if !m.search.active || request.Key() != m.search.requested {
		m.search.worker.submit(&request)
		m.search.requested, m.search.active = request.Key(), true
	}
}

// Exactly one result waiter is scheduled at a time, independently of typing.
func (m model) waitForSearch() tea.Cmd {
	if m.search.worker == nil {
		return nil
	}
	return func() tea.Msg {
		result, ok := <-m.search.worker.results
		if !ok {
			return nil
		}
		return result
	}
}

func (m model) handleSearchResult(msg searchResultMsg) (tea.Model, tea.Cmd) {
	if msg.replacement != nil {
		return m.acceptReplacement(msg), m.waitForSearch()
	}
	if m.replacement != nil || m.quitting {
		return m, m.waitForSearch()
	}
	var cmd tea.Cmd
	request, pending := m.items.PendingSearch()
	if pending && request.Key() == msg.key {
		if msg.err != nil {
			m.err = msg.err
		} else if m.items.AcceptSearch(msg.result) {
			m.finishSnapshotSelection()
			m, cmd = m.confirmSearch(msg.key)
		}
	}
	return m.reflow(), tea.Batch(cmd, m.waitForSearch())
}

func (m model) confirmSearch(key list.SearchKey) (model, tea.Cmd) {
	confirm := m.search.confirm
	m.search.confirm = nil
	current, pending := m.items.PendingSearch()
	if confirm == nil || confirm.key != key || current.Key() != key || pending || m.quitting || m.busy {
		return m, nil
	}
	updated, cmd := m.dispatchAction(confirm.action, list.ItemID{})
	return updated.(model), cmd //nolint:forcetypeassert // action handlers return the concrete model.
}

func (m *model) finishSnapshotSelection() {
	selection := m.search.selection
	if selection == nil || m.items.SearchPending() {
		return
	}
	m.search.selection = nil
	if !m.items.Select(selection.preferred) {
		m.revealCreatedSession(selection.created, selection.pending)
	}
	if selection.pending != nil && isDeleteAction(selection.pending.ActionID) {
		m.selectAfterDelete(selection.pending.ItemID, selection.created, selection.previous)
	}
}

func pendingRowAction(action ActionID) bool {
	switch action {
	case ActionOpen, ActionConfirm, ActionContextMenu, ActionCreateWindow,
		ActionRename, ActionRenameSession, ActionDelete, ActionDeleteSession, ActionUserActions:
		return true
	default:
		return false
	}
}
