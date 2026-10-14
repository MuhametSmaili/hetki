package tui

import (
	"fmt"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/tui/list"
)

const (
	sessionIcon = "\U000f018d"
	windowIcon  = "\ueb14"
)

func projectTree(result backend.StateResult, homeDir string) (list.Snapshot, itemIndex, error) {
	snapshot := list.Snapshot{Items: make([]list.Item, 0, len(result.Sessions))}
	index := make(itemIndex)
	for _, session := range result.Sessions {
		if err := appendTreeSession(&snapshot, index, session, result.Active, homeDir); err != nil {
			return list.Snapshot{}, nil, err
		}
	}
	if err := validateProjection(snapshot, index); err != nil {
		return list.Snapshot{}, nil, err
	}
	return snapshot, index, nil
}

func appendTreeSession(
	snapshot *list.Snapshot,
	index itemIndex,
	session backend.Session,
	active backend.ActiveContext,
	homeDir string,
) error {
	if session.ID == "" {
		return fmt.Errorf("session %q is missing its ID", session.Name)
	}

	sessionID := sessionItemID(session.ID)
	sessionItem := list.Item{
		ID:           sessionID,
		Primary:      sessionIcon + " " + session.Name,
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: session.Name}},
	}
	index[sessionID] = liveItem{
		ID:          sessionID,
		Kind:        liveSession,
		Label:       session.Name,
		Name:        session.Name,
		SessionName: session.Name,
		Target:      backend.Target{SessionID: session.ID},
	}

	activeSession := active.SessionID == session.ID || (active.SessionID == "" && active.Session == session.Name)
	if activeSession {
		snapshot.ActiveItemID = sessionID
	}
	for _, window := range session.Windows {
		if err := appendTreeWindow(&sessionItem, index, session, window, homeDir); err != nil {
			return err
		}
		if activeSession && windowIsActive(window, active) {
			snapshot.ActiveItemID = windowItemID(session.ID, window.ID)
		}
	}
	snapshot.Items = append(snapshot.Items, sessionItem)
	return nil
}

func appendTreeWindow(
	sessionItem *list.Item,
	index itemIndex,
	session backend.Session,
	window backend.Window,
	homeDir string,
) error {
	if window.ID == "" {
		return fmt.Errorf("window %q is missing its ID", window.Name)
	}

	windowID := windowItemID(session.ID, window.ID)
	label := fmt.Sprintf("%d", window.Index)
	if window.Name != "" {
		label += " " + window.Name
	}
	path := displayPath(window.Path, homeDir)
	fields := []list.SearchField{
		{Tier: list.SearchPrimary, Text: label},
		{Tier: list.SearchPrimary, Text: session.Name},
	}
	if path != "" {
		fields = append(fields, list.SearchField{Tier: list.SearchSecondary, Text: path})
	}

	last := session.Last && window.Active
	primary := windowIcon + " " + label
	if last {
		primary += " ↶"
	}
	sessionItem.Children = append(sessionItem.Children, list.Item{
		ID:           windowID,
		Primary:      primary,
		Secondary:    path,
		SearchFields: fields,
	})
	index[windowID] = liveItem{
		ID:           windowID,
		Kind:         liveWindow,
		Label:        label,
		Name:         window.Name,
		SessionName:  session.Name,
		WindowName:   window.Name,
		Target:       backend.Target{SessionID: session.ID, WindowID: window.ID},
		WindowIndex:  window.Index,
		WindowActive: window.Active,
		Last:         last,
		StatusPanes:  paneStatusRefs(window.Panes),
	}
	return nil
}

func windowIsActive(window backend.Window, active backend.ActiveContext) bool {
	return active.WindowID == window.ID || (active.WindowID == "" && active.WindowIndex == window.Index)
}

func sessionItemID(session string) list.ItemID {
	return list.ItemID{ID: session}
}

func windowItemID(session, window string) list.ItemID {
	return list.ItemID{Parent: session, ID: window}
}
