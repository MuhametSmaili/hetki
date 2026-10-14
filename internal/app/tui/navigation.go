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
	record, exists := prepareNavigation(state, item)
	if !exists {
		return ui.ActionResult{}, fmt.Errorf("selected item %q is stale", item.ID)
	}
	a.pendingRecord = record
	return ui.ActionResult{Message: "switching to " + item.Label, Navigation: item.ID}, nil
}

// prepareNavigation reports target membership independently of record availability.
// A nil record with true membership permits navigation without frecency data.
func prepareNavigation(state backend.StateResult, item liveItem) (*navigationRecord, bool) {
	session, found := findSession(state.Sessions, item.Target.SessionID)
	if !found {
		return nil, false
	}

	if item.Kind == liveSession {
		pane, _ := activeWindowPane(session.Windows)
		return navigationRecordForPane(item.ID, session.Name, pane), true
	}
	if item.Kind != liveWindow && item.Kind != liveDestination {
		return nil, false
	}

	window, found := findWindow(session, item.Target.WindowID)
	if !found {
		return nil, false
	}
	if item.Kind == liveWindow {
		pane, _ := activePane(window.Panes)
		return navigationRecordForPane(item.ID, session.Name, pane), true
	}

	for _, pane := range window.Panes {
		if pane.ID == item.Target.PaneID {
			return navigationRecordForPane(item.ID, session.Name, pane), true
		}
	}
	return nil, false
}

func navigationRecordForPane(id list.ItemID, session string, pane backend.Pane) *navigationRecord {
	if pane.Path == "" {
		return nil
	}
	return &navigationRecord{itemID: id, path: pane.Path, session: session}
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
