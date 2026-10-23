package tui

import (
	"fmt"

	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

func contextualMenu(item liveItem) (ui.ItemMenu, error) {
	var openLabel string
	switch item.Kind {
	case liveSession:
		openLabel = "Open session"
	case liveWindow, liveDestination:
		openLabel = "Open pane"
	default:
		return ui.ItemMenu{}, fmt.Errorf("item %q has an unknown kind", item.ID)
	}

	entries := make([]ui.MenuEntry, 0, 9)
	if item.Target.SessionID != "" {
		entries = append(entries, ui.MenuEntry{Action: ui.ActionOpen, Label: openLabel})
	}
	if item.Kind != liveSession && item.Target.WindowID != "" {
		entries = append(entries, ui.MenuEntry{Action: ui.ActionRename, Label: "Rename window"})
	}
	if item.Target.SessionID != "" {
		entries = append(entries,
			ui.MenuEntry{Action: ui.ActionRenameSession, Label: "Rename session"},
			ui.MenuEntry{Action: ui.ActionCreateWindow, Label: "New window"},
		)
	}
	entries = append(entries, ui.MenuEntry{Action: ui.ActionCreateSession, Label: "New session"})
	if item.Kind != liveSession && item.Target.WindowID != "" {
		entries = append(entries, ui.MenuEntry{Action: ui.ActionDelete, Label: "Delete window"})
	}
	if item.Target.SessionID != "" {
		entries = append(entries, ui.MenuEntry{Action: ui.ActionDeleteSession, Label: "Delete session"})
	}
	entries = append(entries,
		ui.MenuEntry{Action: ui.ActionRefresh, Label: "Refresh"},
		ui.MenuEntry{Action: ui.ActionToggleProjection, Label: "Toggle projection"},
	)
	return ui.ItemMenu{Title: "ACTIONS", Entries: entries}, nil
}
