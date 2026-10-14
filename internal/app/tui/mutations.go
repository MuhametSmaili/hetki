package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/MSmaili/hetki/internal/backend"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
)

func owningSessionItem(item liveItem) (liveItem, error) {
	switch item.Kind {
	case liveSession, liveWindow, liveDestination:
	default:
		return liveItem{}, fmt.Errorf("item %q has no owning session action", item.ID)
	}
	if item.Target.SessionID == "" {
		return liveItem{}, fmt.Errorf("item %q has no owning session action", item.ID)
	}
	return liveItem{
		ID: item.ID,

		Kind:        liveSession,
		Label:       item.SessionName,
		Name:        item.SessionName,
		SessionName: item.SessionName,
		Target:      backend.Target{SessionID: item.Target.SessionID},
	}, nil
}

func (a *LiveAdapter) createSession(ctx context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
	if request.Value == nil {
		return ui.ActionResult{Input: &ui.InputPrompt{
			Title: "CREATE SESSION", Prompt: "Session name", SubmitStatus: "creating session...",
		}}, nil
	}
	name := strings.TrimSpace(*request.Value)
	if name == "" {
		return ui.ActionResult{}, fmt.Errorf("session name is required")
	}
	b, err := a.detectBackend()
	if err != nil {
		return ui.ActionResult{}, fmt.Errorf("failed to detect backend: %w", err)
	}
	actions := []backend.Action{backend.CreateSessionAction{Name: name}}
	workspacePath, err := activeWorkspacePath(ctx, b)
	if err != nil {
		return ui.ActionResult{}, err
	}
	if workspacePath != "" {
		actions = append(actions, backend.SetSessionOptionAction{Session: name, Key: backend.WorkspacePathOption, Value: workspacePath})
	}
	if err := b.Apply(ctx, actions); err != nil {
		return ui.ActionResult{}, fmt.Errorf("create session %q: %w", name, err)
	}
	message := "created session " + name
	if workspacePath != "" {
		message += " (workspace linked)"
	}
	return a.refreshAfter(ctx, message, func(snapshot list.Snapshot, index itemIndex) list.ItemID {
		return firstMatchingItem(snapshot, index, func(item liveItem) bool {
			return item.SessionName == name
		})
	})
}

func (a *LiveAdapter) createWindow(ctx context.Context, request ui.ActionRequest, selected liveItem) (ui.ActionResult, error) {
	if request.Value == nil {
		return ui.ActionResult{Input: &ui.InputPrompt{
			Title: "CREATE WINDOW", Prompt: "Window name", SubmitStatus: "creating window...",
		}}, nil
	}
	name := strings.TrimSpace(*request.Value)
	if name == "" {
		return ui.ActionResult{}, fmt.Errorf("window name is required")
	}
	b, err := a.detectBackend()
	if err != nil {
		return ui.ActionResult{}, fmt.Errorf("failed to detect backend: %w", err)
	}
	existing := make(map[list.ItemID]struct{}, len(a.index))
	for _, item := range a.index {
		if !item.windowRowID().IsZero() {
			existing[item.windowRowID()] = struct{}{}
		}
	}
	if err := b.Apply(ctx, []backend.Action{backend.CreateWindowAction{Session: selected.Target.SessionID, Name: name}}); err != nil {
		return ui.ActionResult{}, fmt.Errorf("create window %q in %q: %w", name, selected.Target.SessionID, err)
	}
	return a.refreshAfter(ctx, "created window "+name+" in "+selected.SessionName, func(snapshot list.Snapshot, index itemIndex) list.ItemID {
		return firstMatchingItem(snapshot, index, func(item liveItem) bool {
			_, existed := existing[item.windowRowID()]
			return item.Target.SessionID == selected.Target.SessionID && item.WindowName == name && !existed
		})
	})
}

func (a *LiveAdapter) renameItem(ctx context.Context, request ui.ActionRequest, item liveItem) (ui.ActionResult, error) {
	if request.Value == nil {
		title, prompt, status := "RENAME WINDOW", "Window name", "renaming window..."
		if item.Kind == liveSession {
			title, prompt, status = "RENAME SESSION", "Session name", "renaming session..."
		}
		return ui.ActionResult{Input: &ui.InputPrompt{
			Title: title, Prompt: prompt, InitialValue: item.Name, SubmitStatus: status,
		}}, nil
	}
	name := strings.TrimSpace(*request.Value)
	if name == "" {
		return ui.ActionResult{}, fmt.Errorf("new name is required")
	}
	b, err := a.detectBackend()
	if err != nil {
		return ui.ActionResult{}, fmt.Errorf("failed to detect backend: %w", err)
	}
	var action backend.Action
	var message string
	if item.Kind == liveSession {
		action = backend.RenameSessionAction{Current: item.Target.SessionID, New: name}
		message = "renamed session " + item.SessionName + " -> " + name
	} else {
		action = backend.RenameWindowAction{
			Session: item.Target.SessionID, Window: item.Target.WindowID, WindowID: item.Target.WindowID, New: name,
		}
		message = "renamed window " + item.WindowName + " -> " + name
	}
	if err := b.Apply(ctx, []backend.Action{action}); err != nil {
		return ui.ActionResult{}, fmt.Errorf("rename item %q: %w", item.ID, err)
	}
	return a.refreshAfter(ctx, message, func(list.Snapshot, itemIndex) list.ItemID { return item.ID })
}

func (a *LiveAdapter) deleteItem(ctx context.Context, request ui.ActionRequest, item liveItem) (ui.ActionResult, error) {
	if !request.Confirmed {
		title, noun, status := "DELETE WINDOW", "window", "deleting window..."
		if item.Kind == liveSession {
			title, noun, status = "DELETE SESSION", "session", "deleting session..."
		}
		return ui.ActionResult{Confirmation: &ui.Confirmation{
			Title: title, Body: fmt.Sprintf("Delete %s %q?", noun, item.Label), SubmitStatus: status,
		}}, nil
	}
	b, err := a.detectBackend()
	if err != nil {
		return ui.ActionResult{}, fmt.Errorf("failed to detect backend: %w", err)
	}
	var action backend.Action
	var message string
	preferred := item.parentRowID()
	if item.Kind == liveSession {
		action = backend.KillSessionAction{Name: item.Target.SessionID}
		message = "deleted session " + item.SessionName
		preferred = list.ItemID{}
	} else {
		action = backend.KillWindowAction{
			Session: item.Target.SessionID, Window: item.Target.WindowID, WindowID: item.Target.WindowID,
		}
		message = "deleted window " + item.WindowName
	}
	if err := b.Apply(ctx, []backend.Action{action}); err != nil {
		return ui.ActionResult{}, fmt.Errorf("delete item %q: %w", item.ID, err)
	}
	return a.refreshAfter(ctx, message, func(list.Snapshot, itemIndex) list.ItemID { return preferred })
}

func (a *LiveAdapter) refreshAfter(ctx context.Context, message string, selectItem func(list.Snapshot, itemIndex) list.ItemID) (ui.ActionResult, error) {
	snapshot, err := a.loadSnapshot(ctx)
	if err != nil {
		return ui.ActionResult{}, err
	}
	return ui.ActionResult{Message: message, Snapshot: &snapshot, SelectItemID: selectItem(snapshot, a.index)}, nil
}

func workspacePathForSession(sessions []backend.Session, sessionName string) string {
	for _, session := range sessions {
		if session.Name == sessionName {
			return session.WorkspacePath
		}
	}
	return ""
}

func activeWorkspacePath(ctx context.Context, b backend.Backend) (string, error) {
	state, err := b.QueryState(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to query sessions for workspace inheritance: %w", err)
	}
	return strings.TrimSpace(workspacePathForSession(state.Sessions, state.Active.Session)), nil
}
