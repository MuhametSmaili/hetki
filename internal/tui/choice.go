package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// ChoiceEvent identifies a transition requested from the application-owned flow.
// The zero value opens a flow; nonempty values are validated by its dispatcher.
type ChoiceEvent string

const (
	ChoiceSelect  ChoiceEvent = "select"
	ChoiceBack    ChoiceEvent = "back"
	ChoiceRefresh ChoiceEvent = "refresh"
)

// ChoiceScreen describes a generic screen. The application owns its data and
// transitions; the TUI retains only per-screen query and cursor positions.
type ChoiceScreen struct {
	Key    string
	Title  string
	Closed bool
	// ResetHistory discards other screens' positions, retaining this screen's.
	ResetHistory bool
}

type choicePosition struct {
	query    string
	selected list.ItemID
}

type choiceReturn struct {
	items   list.Model
	view    ViewMode
	scope   Scope
	preview bool
	mode    uiMode
}

type choiceState struct {
	screen     ChoiceScreen
	history    map[string]choicePosition
	parent     *choiceReturn
	loadFailed bool
}

func handleUserActions(m model, id list.ItemID) (tea.Model, tea.Cmd) {
	return m.startItemAction(ActionUserActions, id)
}

func (m model) dispatchChoice(action ActionID, id list.ItemID) (tea.Model, tea.Cmd) {
	switch action {
	case ActionOpen, ActionConfirm:
		if id.IsZero() {
			row, ok := m.items.Selected()
			if !ok {
				return m, nil
			}
			id = row.Item.ID
		}
		return m.startChoice(ChoiceSelect, id)
	case ActionCancel, ActionCollapse:
		return m.startChoice(ChoiceBack, list.ItemID{})
	case ActionRefresh:
		return m.startChoice(ChoiceRefresh, list.ItemID{})
	case ActionClearFilter:
		m.setQuery("")
		m.mode = modeFilter
		return m.reflow(), nil
	case ActionQuit, ActionMoveUp, ActionMoveDown, ActionMoveTop, ActionMoveBottom,
		ActionPageUp, ActionPageDown, ActionFilter, ActionNextMatch, ActionPrevMatch,
		ActionBackspace, ActionDeleteWord, ActionDeleteToStart:
		return m.dispatchRegularAction(action, id)
	default:
		return m.actionUnavailable(action)
	}
}

func (m model) startChoice(event ChoiceEvent, id list.ItemID) (tea.Model, tea.Cmd) {
	row, _ := m.items.Selected()
	m.choice.history[m.choice.screen.Key] = choicePosition{query: m.items.Query(), selected: row.Item.ID}
	return m.startRequest(ActionRequest{ActionID: ActionUserActions, Choice: event, ItemID: id}, statusRunningAction)
}

func (m model) applyChoiceResult(result ActionResult, actionErr error) (tea.Model, tea.Cmd) {
	if result.Choice.Closed {
		return m.closeChoice(result)
	}
	if result.Snapshot == nil {
		m.err = fmt.Errorf("choice screen has no snapshot")
		return m, nil
	}
	var position choicePosition
	if m.choice != nil {
		position = m.choice.history[result.Choice.Key]
	}
	items, err := prepareChoiceItems(m.ctx, *result.Snapshot, position)
	if err != nil {
		m.err = err
		return m, nil
	}
	if m.choice == nil {
		m.choice = &choiceState{
			history: make(map[string]choicePosition),
			parent:  &choiceReturn{items: m.items, view: m.view, scope: m.scope.mode, preview: m.preview.enabled, mode: m.mode},
		}
	}
	if result.Choice.ResetHistory {
		m.choice.history = map[string]choicePosition{result.Choice.Key: position}
	}
	if m.search.worker != nil {
		items.DeferSearch()
	}
	m.items = items
	m.scope.valid = false
	m.scope.changeDue = time.Time{}
	m.search.selection = nil
	m.choice.screen = *result.Choice
	m.choice.loadFailed = actionErr != nil && len(result.Snapshot.Items) == 0
	m.mode, m.view, m.scope.mode, m.preview.enabled = modeFilter, ViewFlat, ScopeAll, false
	m.preview.invalidate()
	m.err = actionErr
	return m.reflow(), nil
}

func prepareChoiceItems(ctx context.Context, snapshot list.Snapshot, position choicePosition) (list.Model, error) {
	return (list.Model{}).Prepare(ctx, func(items *list.Model) error {
		if err := items.Replace(snapshot, list.ItemID{}); err != nil {
			return err
		}
		items.SetQuery(position.query)
		items.PreferSelection(position.selected)
		return nil
	})
}

func (m model) closeChoice(result ActionResult) (tea.Model, tea.Cmd) {
	if m.choice == nil || m.choice.parent == nil {
		m.quitting = true
		return m, tea.Quit
	}
	parent := m.choice.parent
	m.items, m.view, m.scope.mode, m.preview.enabled = parent.items, parent.view, parent.scope, parent.preview
	m.choice = nil
	m.scope.valid = false
	m.search.selection = nil
	m.mode = parent.mode
	if result.Snapshot != nil {
		return m.applyActionSnapshot(result, nil, nil)
	}
	return m.reflow(), nil
}

func (m model) choiceHeader(layout layoutMetrics) string {
	message := headerMessage(m)
	if message == "" {
		message = m.choice.screen.Title + " · Esc back"
	}
	header := renderSearchBar(searchBarProps{
		Width: layout.listWidth, Filter: m.items.Query(), Active: m.mode == modeFilter,
		Prompt: ">", Meta: truncateWidth(message, max(0, layout.listWidth/2)), Theme: m.theme,
	})
	if layout.headerHeight > 1 {
		header += "\n" + renderHeaderRule(layout.listWidth, "", m.theme)
	}
	return strings.TrimSuffix(header, "\n")
}

func (m model) choiceEmptyState() string {
	if m.busy {
		return "loading choices…"
	}
	message, action, verb := "no choices available", ActionRefresh, "refresh"
	if m.choice.loadFailed {
		message, verb = "could not load choices", "retry"
	} else if len(m.items.Snapshot().Items) > 0 {
		message, action, verb = "no matching choices", ActionClearFilter, "clear filter"
	}
	if key := m.choiceActionKey(action); key != "" {
		message += " · " + key + " " + verb
	} else if action == ActionClearFilter {
		message += " · edit filter"
	}
	return message + " · Esc back"
}

func (m model) choiceActionKey(action ActionID) string {
	mode := keyModeFor(m.mode)
	for _, key := range m.keys.Keys(mode, action) {
		if key != "esc" {
			return key
		}
	}
	if action == ActionRefresh {
		_, bound := m.keys.Action(mode, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
		if !bound {
			return "ctrl+r"
		}
	}
	return ""
}
