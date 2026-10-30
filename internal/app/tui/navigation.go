package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/backend"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type preparedNavigation struct {
	itemID  list.ItemID
	target  backend.Target
	path    string
	session string
}

func (item liveItem) openTarget(paneID string) backend.Target {
	target := item.Target
	if item.Kind != liveWindow {
		return target
	}

	target.PaneID = item.OpenPaneID
	if paneID != "" {
		target.PaneID = paneID
	}
	return target
}

func (a *LiveAdapter) openItem(ctx context.Context, item liveItem) (ui.ActionResult, error) {
	a.pendingNavigation = nil

	if item.Target.SessionID == "" {
		return ui.ActionResult{}, fmt.Errorf("item %q has no navigation target", item.ID)
	}

	b, err := a.detectBackend()
	if err != nil {
		return ui.ActionResult{}, fmt.Errorf("failed to detect backend: %w", err)
	}

	state, err := b.QueryState(ctx)
	if err != nil {
		return ui.ActionResult{}, fmt.Errorf("validate selected item %q: %w", item.ID, err)
	}

	return a.prepareItemNavigation(state, item)
}

func (a *LiveAdapter) openLastSession(ctx context.Context) (ui.ActionResult, error) {
	a.pendingNavigation = nil

	state, err := a.queryState(ctx)
	if err != nil {
		return ui.ActionResult{}, err
	}

	a.mu.RLock()
	input, view := a.input, a.projection
	a.mu.RUnlock()
	if input == nil {
		return ui.ActionResult{}, errors.New("live state is not loaded")
	}

	// Reuse loaded scores so refreshing history does not reload or rerank frecency.
	prepared, err := newProjectionInput(state, input.scores, input.notice).project(view)
	if err != nil {
		return ui.ActionResult{}, err
	}

	var last liveItem
	for _, item := range prepared.index {
		if item.Last {
			last = item
			break
		}
	}
	if last.ID.IsZero() {
		return ui.ActionResult{}, errors.New("no previous session available")
	}

	result, err := a.prepareItemNavigation(state.StateResult, last)
	if err != nil {
		return ui.ActionResult{}, err
	}
	if err := a.publishSnapshot(ctx, prepared); err != nil {
		a.pendingNavigation = nil
		return ui.ActionResult{}, err
	}

	result.Snapshot = &prepared.snapshot
	return result, nil
}

func (a *LiveAdapter) prepareItemNavigation(state backend.StateResult, item liveItem) (ui.ActionResult, error) {
	navigation := prepareNavigation(state, item)
	if navigation == nil {
		return ui.ActionResult{}, fmt.Errorf("selected item %q is stale", item.ID)
	}

	a.pendingNavigation = navigation
	return ui.ActionResult{Message: "switching to " + item.Label, Navigation: item.ID}, nil
}

func prepareNavigation(state backend.StateResult, item liveItem) *preparedNavigation {
	session, found := findSession(state.Sessions, item.Target.SessionID)
	if !found {
		return nil
	}

	if item.Kind == liveSession {
		pane, _ := activeWindowPane(session.Windows)
		return navigationForPane(item, session.Name, pane)
	}
	if item.Kind != liveWindow && item.Kind != liveDestination {
		return nil
	}

	window, found := findWindow(session, item.Target.WindowID)
	if !found {
		return nil
	}

	if item.Kind == liveWindow && item.Target.PaneID == "" {
		pane, _ := activePane(window.Panes)
		return navigationForPane(item, session.Name, pane)
	}

	for _, pane := range window.Panes {
		if pane.ID == item.Target.PaneID {
			return navigationForPane(item, session.Name, pane)
		}
	}
	return nil
}

func navigationForPane(item liveItem, session string, pane backend.Pane) *preparedNavigation {
	return &preparedNavigation{
		itemID:  item.ID,
		target:  item.Target,
		path:    pane.Path,
		session: session,
	}
}

func findSession(sessions []backend.Session, target string) (backend.Session, bool) {
	for _, session := range sessions {
		if session.ID == target {
			return session, true
		}
	}
	return backend.Session{}, false
}

func findWindow(session backend.Session, target string) (backend.Window, bool) {
	for _, window := range session.Windows {
		if window.ID == target {
			return window, true
		}
	}
	return backend.Window{}, false
}

func activeWindowPane(windows []backend.Window) (backend.Pane, bool) {
	for _, window := range windows {
		if !window.Active {
			continue
		}
		if pane, found := activePane(window.Panes); found && pane.Path != "" {
			return pane, true
		}
	}
	return backend.Pane{}, false
}

func activePane(panes []backend.Pane) (backend.Pane, bool) {
	for _, pane := range panes {
		if pane.Active {
			return pane, true
		}
	}
	return backend.Pane{}, false
}

// Navigate consumes the prepared Open or LastSession target.
func (a *LiveAdapter) Navigate(ctx context.Context, navigation list.ItemID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	prepared := a.pendingNavigation
	a.pendingNavigation = nil
	if prepared == nil || prepared.itemID != navigation {
		return fmt.Errorf("navigation to item %q was not prepared", navigation)
	}

	b, err := a.detectBackend()
	if err != nil {
		return fmt.Errorf("failed to detect backend: %w", err)
	}

	if err := b.Switch(ctx, prepared.target); err != nil {
		return fmt.Errorf("switch to item %q: %w", navigation, err)
	}

	if prepared.path != "" && a.frecency != nil {
		if err := a.frecency.Record(ctx, prepared.path, prepared.session); err != nil {
			return fmt.Errorf("record navigation to item %q: %w", navigation, err)
		}
	}
	return nil
}
