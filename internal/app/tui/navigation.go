package tui

import (
	"context"
	"fmt"

	"github.com/MSmaili/hetki/internal/backend"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
)

type navigationRecord struct {
	itemID  list.ItemID
	path    string
	session string
}

func (a *LiveAdapter) openItem(ctx context.Context, item liveItem) (ui.ActionResult, error) {
	a.pendingRecord = nil
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
	if !liveItemExists(state, item) {
		return ui.ActionResult{}, fmt.Errorf("selected item %q is stale", item.ID)
	}
	if record, ok := navigationRecordForItem(state, item); ok {
		a.pendingRecord = &record
	}
	return ui.ActionResult{Message: "switching to " + item.Label, Navigation: item.ID}, nil
}

func liveItemExists(state backend.StateResult, item liveItem) bool {
	session, found := findSession(state.Sessions, item.Target.SessionID)
	if !found {
		return false
	}
	if item.Kind == liveSession {
		return item.Target.SessionID == session.ID
	}

	if item.Kind == liveWindow {
		_, found := findWindow(session, item.Target.WindowID)
		return found
	}
	if item.Kind != liveDestination {
		return false
	}

	window, found := findWindow(session, item.Target.WindowID)
	if !found {
		return false
	}
	for _, pane := range window.Panes {
		if pane.ID == item.Target.PaneID {
			return true
		}
	}
	return false
}

func navigationRecordForItem(state backend.StateResult, item liveItem) (navigationRecord, bool) {
	session, found := findSession(state.Sessions, item.Target.SessionID)
	if !found {
		return navigationRecord{}, false
	}

	if item.Kind == liveSession {
		pane, found := activeWindowPane(session.Windows)
		if !found {
			return navigationRecord{}, false
		}
		return navigationRecord{itemID: item.ID, path: pane.Path, session: session.Name}, true
	}

	window, found := findWindow(session, item.Target.WindowID)
	if !found {
		return navigationRecord{}, false
	}
	if item.Kind == liveDestination {
		for _, pane := range window.Panes {
			if pane.ID == item.Target.PaneID && pane.Path != "" {
				return navigationRecord{itemID: item.ID, path: pane.Path, session: session.Name}, true
			}
		}
		return navigationRecord{}, false
	}
	pane, found := activePane(window.Panes)
	if !found || pane.Path == "" {
		return navigationRecord{}, false
	}
	return navigationRecord{itemID: item.ID, path: pane.Path, session: session.Name}, true
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

func (a *LiveAdapter) Navigate(ctx context.Context, navigation list.ItemID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	record := a.pendingRecord
	a.pendingRecord = nil
	item, err := a.resolveItem(navigation)
	if err != nil {
		return err
	}

	b, err := a.detectBackend()
	if err != nil {
		return fmt.Errorf("failed to detect backend: %w", err)
	}
	if err := b.Switch(ctx, item.Target); err != nil {
		return fmt.Errorf("switch to item %q: %w", item.ID, err)
	}
	if record != nil && record.itemID == item.ID && a.frecency != nil {
		if err := a.frecency.Record(ctx, record.path, record.session); err != nil {
			return fmt.Errorf("record navigation to item %q: %w", item.ID, err)
		}
	}
	return nil
}
