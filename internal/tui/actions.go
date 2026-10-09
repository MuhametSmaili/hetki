package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/MSmaili/hetki/internal/tui/list"
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
	ActionJump             ActionID = "jump"
	ActionNextMatch        ActionID = "next_match"
	ActionPrevMatch        ActionID = "previous_match"
	ActionClearFilter      ActionID = "clear_filter"
	ActionContextMenu      ActionID = "context_menu"
	ActionCreateSession    ActionID = "create_session"
	ActionCreateWindow     ActionID = "create_window"
	ActionRename           ActionID = "rename"
	ActionRenameSession    ActionID = "rename_session"
	ActionDelete           ActionID = "delete"
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
	ActionTogglePreview    ActionID = "toggle_preview"
	ActionOpen             ActionID = "open"
	ActionLastSession      ActionID = "last_session"
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
	ActionJump,
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
	ActionTogglePreview,
	ActionOpen,
	ActionLastSession,
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
		ActionJump:             handleJump,
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
		ActionTogglePreview:    handleTogglePreview,
		ActionOpen:             handleOpen,
		ActionLastSession:      handleLastSession,
	}
}

func (m model) dispatchAction(action ActionID, itemID list.ItemID) (tea.Model, tea.Cmd) {
	handler, ok := actionHandlers[action]
	if !ok {
		m.err = fmt.Errorf("action %q is not implemented", action)
		return m, nil
	}
	return handler(m, itemID)
}

type BackendTarget string

type ActionRequest struct {
	ActionID  ActionID
	ItemID    list.ItemID
	Value     *string
	Confirmed bool
}

type InputPrompt struct {
	Title        string
	Prompt       string
	InitialValue string
	SubmitStatus string
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
	Message        string
	Snapshot       *list.Snapshot
	StatusBindings StatusBindings
	SelectItemID   list.ItemID
	Input          *InputPrompt
	Confirmation   *Confirmation
	Menu           *ItemMenu
	Navigation     BackendTarget
}
