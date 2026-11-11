package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type ActionID string

const (
	ActionQuit             ActionID = "quit"
	ActionMoveUp           ActionID = "move_up"
	ActionMoveDown         ActionID = "move_down"
	ActionMoveTop          ActionID = "move_top"
	ActionMoveBottom       ActionID = "move_bottom"
	ActionPageUp           ActionID = "page_up"
	ActionPageDown         ActionID = "page_down"
	ActionFilter           ActionID = "filter"
	ActionNextMatch        ActionID = "next_match"
	ActionPrevMatch        ActionID = "previous_match"
	ActionClearFilter      ActionID = "clear_filter"
	ActionContextMenu      ActionID = "context_menu"
	ActionCreateSession    ActionID = "create_session"
	ActionCreateWindow     ActionID = "create_window"
	ActionRename           ActionID = "rename_window"
	ActionRenameSession    ActionID = "rename_session"
	ActionDelete           ActionID = "delete_window"
	ActionDeleteSession    ActionID = "delete_session"
	ActionExpand           ActionID = "expand"
	ActionCollapse         ActionID = "collapse"
	ActionExpandAll        ActionID = "expand_all"
	ActionCollapseAll      ActionID = "collapse_all"
	ActionBackspace        ActionID = "backspace"
	ActionDeleteWord       ActionID = "delete_word"
	ActionDeleteToStart    ActionID = "delete_to_start"
	ActionCancel           ActionID = "cancel"
	ActionConfirm          ActionID = "confirm"
	ActionRefresh          ActionID = "refresh"
	ActionToggleProjection ActionID = "toggle_projection"
	ActionToggleScope      ActionID = "toggle_scope"
	ActionTogglePreview    ActionID = "toggle_preview"
	ActionOpen             ActionID = "open"
	ActionLastSession      ActionID = "last_session"
	ActionUserActions      ActionID = "user_actions"
)

// declaredActions is the authoritative action set checked against actionHandlers.
var declaredActions = [...]ActionID{
	ActionQuit,
	ActionMoveUp,
	ActionMoveDown,
	ActionMoveTop,
	ActionMoveBottom,
	ActionPageUp,
	ActionPageDown,
	ActionFilter,
	ActionNextMatch,
	ActionPrevMatch,
	ActionClearFilter,
	ActionContextMenu,
	ActionCreateSession,
	ActionCreateWindow,
	ActionRename,
	ActionRenameSession,
	ActionDelete,
	ActionDeleteSession,
	ActionExpand,
	ActionCollapse,
	ActionExpandAll,
	ActionCollapseAll,
	ActionBackspace,
	ActionDeleteWord,
	ActionDeleteToStart,
	ActionCancel,
	ActionConfirm,
	ActionRefresh,
	ActionToggleProjection,
	ActionToggleScope,
	ActionTogglePreview,
	ActionOpen,
	ActionLastSession,
	ActionUserActions,
}

type actionHandler func(model, list.ItemID) (tea.Model, tea.Cmd)

var actionHandlers map[ActionID]actionHandler

func init() {
	actionHandlers = map[ActionID]actionHandler{
		ActionQuit:             handleQuit,
		ActionMoveUp:           handleMoveUp,
		ActionMoveDown:         handleMoveDown,
		ActionMoveTop:          handleMoveTop,
		ActionMoveBottom:       handleMoveBottom,
		ActionPageUp:           handlePageUp,
		ActionPageDown:         handlePageDown,
		ActionFilter:           handleFilter,
		ActionNextMatch:        handleNextMatch,
		ActionPrevMatch:        handlePrevMatch,
		ActionClearFilter:      handleClearFilter,
		ActionContextMenu:      handleContextMenu,
		ActionCreateSession:    handleCreateSession,
		ActionCreateWindow:     handleCreateWindow,
		ActionRename:           handleRename,
		ActionRenameSession:    handleRenameSession,
		ActionDelete:           handleDelete,
		ActionDeleteSession:    handleDeleteSession,
		ActionExpand:           handleExpand,
		ActionCollapse:         handleCollapse,
		ActionExpandAll:        handleExpandAll,
		ActionCollapseAll:      handleCollapseAll,
		ActionBackspace:        handleBackspace,
		ActionDeleteWord:       handleDeleteWord,
		ActionDeleteToStart:    handleDeleteToStart,
		ActionCancel:           handleCancel,
		ActionConfirm:          handleConfirm,
		ActionRefresh:          handleRefresh,
		ActionToggleProjection: handleToggleProjection,
		ActionToggleScope:      handleToggleScope,
		ActionTogglePreview:    handleTogglePreview,
		ActionOpen:             handleOpen,
		ActionLastSession:      handleLastSession,
		ActionUserActions:      handleUserActions,
	}
}

func (m model) dispatchAction(action ActionID, itemID list.ItemID) (tea.Model, tea.Cmd) {
	m.search.confirm = nil
	if m.items.SearchPending() && m.isListMode() && pendingRowAction(action) {
		if itemID.IsZero() && (action == ActionOpen || action == ActionConfirm) {
			request, _ := m.items.PendingSearch()
			m.search.confirm = &searchConfirmation{key: request.Key(), action: action}
		}
		return m, nil
	}
	if m.choice != nil {
		return m.dispatchChoice(action, itemID)
	}
	return m.dispatchRegularAction(action, itemID)
}

func (m model) dispatchRegularAction(action ActionID, itemID list.ItemID) (tea.Model, tea.Cmd) {
	handler, ok := actionHandlers[action]
	if !ok {
		m.err = fmt.Errorf("action %q is not implemented", action)
		return m, nil
	}
	return handler(m, itemID)
}

type ActionRequest struct {
	ActionID ActionID
	ItemID   list.ItemID
	Value    *string
	// Values is nil before submission. An empty map still means submission.
	Values    map[string]string
	Confirmed bool
	Choice    ChoiceEvent

	// PaneID overrides a window's Open target, never its mutation target.
	PaneID string
}

type InputPrompt struct {
	Title        string
	Prompt       string
	InitialValue string
	SubmitStatus string
	// Fields replaces Prompt and InitialValue when it has entries.
	Fields      []InputField
	SubmitLabel string
}

// InputKind selects text input or directory input.
type InputKind string

const (
	InputText      InputKind = "text"
	InputDirectory InputKind = "directory"
)

// InputField describes a text or directory field.
type InputField struct {
	ID           string
	Label        string
	Kind         InputKind
	InitialValue string
	Placeholder  string
	Required     bool
	// BasePath is the fixed base directory for relative paths.
	BasePath string
}

type Confirmation struct {
	Title        string
	Body         string
	SubmitStatus string
}

type MenuEntry struct {
	Action ActionID
	Label  string
}

type ItemMenu struct {
	Title   string
	Entries []MenuEntry
}

type ActionResult struct {
	Choice *ChoiceScreen
	// View is committed only with an accepted Snapshot; empty leaves it unchanged.
	View         ViewMode
	Message      string
	Snapshot     *list.Snapshot
	Status       *StatusUpdate
	SelectItemID list.ItemID
	Input        *InputPrompt
	Confirmation *Confirmation
	Menu         *ItemMenu
	Navigation   list.ItemID
}
