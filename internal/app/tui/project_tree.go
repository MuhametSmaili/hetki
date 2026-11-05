package tui

import (
	"fmt"
	"strconv"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
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
		Primary:      session.Name,
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
			paneID := destinationItemID(session.ID, window.ID, active.PaneID)
			if _, exists := index[paneID]; exists {
				snapshot.ActiveItemID = paneID
			}
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

	split := len(window.Panes) > 1
	pane, _ := activePane(window.Panes)
	if split {
		pane = backend.Pane{}
	}
	details := treePaneDetails(session, label, pane, homeDir)

	last := session.Last && window.Active
	primary := label
	if last {
		primary += " ↶"
	}
	item := list.Item{
		ID:           windowID,
		Primary:      primary,
		Secondary:    details.Secondary,
		Trailing:     details.Trailing,
		SearchFields: details.SearchFields,
	}
	if split {
		if err := appendTreePanes(&item, index, session, window, homeDir); err != nil {
			return err
		}
	}
	sessionItem.Children = append(sessionItem.Children, item)
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
		OpenPaneID:   pane.ID,
		PaneRows:     split,
		Last:         last,
	}
	return nil
}

func appendTreePanes(item *list.Item, index itemIndex, session backend.Session, window backend.Window, homeDir string) error {
	for _, pane := range window.Panes {
		child, destination, err := projectPaneDestination(session, window, pane, homeDir)
		if err != nil {
			return err
		}

		child.Primary = strconv.Itoa(pane.Index)
		child.SearchFields = append([]list.SearchField{{Tier: list.SearchPrimary, Text: child.Primary}}, child.SearchFields...)
		item.Children = append(item.Children, child)
		index[child.ID] = destination
	}
	return nil
}

func treePaneDetails(session backend.Session, label string, pane backend.Pane, homeDir string) list.Details {
	path := displayPath(pane.Path, homeDir)
	program := paneProgramLabel(pane, session.DefaultShell)
	if pane.ID == "" {
		program = ""
	}
	fields := []list.SearchField{
		{Tier: list.SearchPrimary, Text: label},
		{Tier: list.SearchPrimary, Text: session.Name},
	}
	if program != "" {
		fields = append(fields, list.SearchField{Tier: list.SearchPrimary, Text: program})
	}
	if path != "" {
		fields = append(fields, list.SearchField{Tier: list.SearchSecondary, Text: path})
	}
	return list.Details{Secondary: path, Trailing: program, SearchFields: fields}
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
