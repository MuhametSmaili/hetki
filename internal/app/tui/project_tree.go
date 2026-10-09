package tui

import (
	"fmt"
	"strconv"
	"strings"

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
	if err := validateStableTmuxID(session.ID, '$', "session"); err != nil {
		return err
	}

	sessionID := list.ItemID("session:" + session.ID)
	sessionItem := list.Item{
		ID:           sessionID,
		Primary:      sessionIcon + " " + session.Name,
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: session.Name}},
	}
	index[sessionID] = liveItem{
		ID:             sessionID,
		SessionID:      sessionID,
		Kind:           liveSession,
		Label:          session.Name,
		Name:           session.Name,
		SessionName:    session.Name,
		Target:         session.ID,
		MutationTarget: session.ID,
		SessionTarget:  session.ID,
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
	if err := validateStableTmuxID(window.ID, '@', "window"); err != nil {
		return err
	}

	windowID := windowItemID(session.ID, window.ID)
	label := fmt.Sprintf("%d", window.Index)
	if window.Name != "" {
		label += " " + window.Name
	}
	path := displayPath(window.Path, homeDir)
	fields := []list.SearchField{{Tier: list.SearchPrimary, Text: label}}
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
	mutationTarget := session.ID + ":" + window.ID
	index[windowID] = liveItem{
		ID:             windowID,
		ParentID:       sessionItem.ID,
		SessionID:      sessionItem.ID,
		WindowID:       windowID,
		Kind:           liveWindow,
		Label:          label,
		Name:           window.Name,
		SessionName:    session.Name,
		WindowName:     window.Name,
		Target:         mutationTarget,
		MutationTarget: mutationTarget,
		SessionTarget:  session.ID,
		WindowIndex:    window.Index,
		WindowActive:   window.Active,
		Last:           last,
		StatusPanes:    paneStatusRefs(window.Panes),
	}
	return nil
}

func windowIsActive(window backend.Window, active backend.ActiveContext) bool {
	return active.WindowID == window.ID || (active.WindowID == "" && active.WindowIndex == window.Index)
}

func windowItemID(session, window string) list.ItemID {
	return list.ItemID("window:" + session + ":" + window)
}

func validateStableTmuxID(value string, prefix byte, kind string) error {
	text := strings.TrimPrefix(value, string(prefix))
	id, err := strconv.Atoi(text)
	if !strings.HasPrefix(value, string(prefix)) || err != nil || id < 0 || strconv.Itoa(id) != text {
		return fmt.Errorf("%s must have a stable %cN ID, got %q", kind, prefix, value)
	}
	return nil
}
