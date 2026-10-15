package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/frecency"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

func projectFlat(result backend.StateResult, homeDir string) (list.Snapshot, itemIndex, error) {
	return projectFlatRanked(result, homeDir, frecency.Scores{})
}

func projectFlatRanked(
	result backend.StateResult,
	homeDir string,
	scores frecency.Scores,
) (list.Snapshot, itemIndex, error) {
	snapshot := list.Snapshot{}
	index := make(itemIndex)

	for _, session := range result.Sessions {
		if err := appendFlatSession(&snapshot, index, session, result.Active, homeDir); err != nil {
			return list.Snapshot{}, nil, err
		}
	}

	sort.Slice(snapshot.Items, func(i, j int) bool {
		left := index[snapshot.Items[i].ID]
		right := index[snapshot.Items[j].ID]
		return destinationLess(left, right, scores)
	})
	if err := validateProjection(snapshot, index); err != nil {
		return list.Snapshot{}, nil, err
	}
	return snapshot, index, nil
}

func appendFlatSession(
	snapshot *list.Snapshot,
	index itemIndex,
	session backend.Session,
	active backend.ActiveContext,
	homeDir string,
) error {
	if session.ID == "" {
		return fmt.Errorf("session %q is missing its ID", session.Name)
	}
	for _, window := range session.Windows {
		if err := appendFlatWindow(snapshot, index, session, window, active, homeDir); err != nil {
			return err
		}
	}
	return nil
}

func appendFlatWindow(
	snapshot *list.Snapshot,
	index itemIndex,
	session backend.Session,
	window backend.Window,
	active backend.ActiveContext,
	homeDir string,
) error {
	if window.ID == "" {
		return fmt.Errorf("window %q is missing its ID", window.Name)
	}
	for _, pane := range window.Panes {
		item, destination, err := projectFlatDestination(session, window, pane, homeDir)
		if err != nil {
			return err
		}
		snapshot.Items = append(snapshot.Items, item)
		index[item.ID] = destination
		if item.ID == destinationItemID(active.SessionID, active.WindowID, active.PaneID) {
			snapshot.ActiveItemID = item.ID
		}
	}
	return nil
}

func projectFlatDestination(
	session backend.Session,
	window backend.Window,
	pane backend.Pane,
	homeDir string,
) (list.Item, liveItem, error) {
	if pane.ID == "" {
		return list.Item{}, liveItem{}, fmt.Errorf("pane is missing its ID")
	}

	name := window.Name
	if strings.TrimSpace(name) == "" {
		name = strconv.Itoa(window.Index)
	}
	id := destinationItemID(session.ID, window.ID, pane.ID)
	fields := []list.SearchField{
		{Tier: list.SearchPrimary, Text: session.Name},
		{Tier: list.SearchPrimary, Text: name},
	}
	program := paneProgramLabel(pane, session.DefaultShell)
	hideCommand := shouldHideCommand(pane.Command, session.DefaultShell)
	if pane.Command != "" && !hideCommand {
		fields = append(fields, list.SearchField{Tier: list.SearchPrimary, Text: pane.Command})
	}
	if pane.Program != "" && (pane.Program != pane.Command || hideCommand) {
		fields = append(fields, list.SearchField{Tier: list.SearchPrimary, Text: pane.Program})
	}
	fields = appendPathSearchFields(fields, pane.Path, homeDir)

	item := list.Item{
		ID:           id,
		Context:      session.Name,
		Primary:      name,
		Secondary:    displayPath(pane.Path, homeDir),
		Trailing:     program,
		SearchFields: fields,
	}
	destination := liveItem{
		ID:           id,
		Kind:         liveDestination,
		Label:        name,
		Name:         window.Name,
		SessionName:  session.Name,
		WindowName:   window.Name,
		Target:       backend.Target{SessionID: session.ID, WindowID: window.ID, PaneID: pane.ID},
		RawPath:      pane.Path,
		WindowIndex:  window.Index,
		WindowActive: window.Active,
		PaneActive:   pane.Active,
		PaneIndex:    pane.Index,
		Last:         session.Last && window.Active && pane.Active,
	}
	if len(window.Panes) > 1 {
		item.Annotation = "·" + strconv.Itoa(pane.Index)
	}

	if destination.Last {
		item.Primary += " ↶"
	}
	return item, destination, nil
}

func destinationLess(left, right liveItem, scores frecency.Scores) bool {
	leftScore := scores.Path(left.RawPath)
	rightScore := scores.Path(right.RawPath)
	if leftScore != rightScore {
		return leftScore > rightScore
	}

	leftScore = scores.Record(left.RawPath, left.SessionName)
	rightScore = scores.Record(right.RawPath, right.SessionName)
	if leftScore != rightScore {
		return leftScore > rightScore
	}
	if left.SessionName != right.SessionName {
		return left.SessionName < right.SessionName
	}
	if left.WindowIndex != right.WindowIndex {
		return left.WindowIndex < right.WindowIndex
	}
	if left.RawPath != right.RawPath {
		return left.RawPath < right.RawPath
	}
	if left.Target.SessionID != right.Target.SessionID {
		return left.Target.SessionID < right.Target.SessionID
	}
	if left.Target.WindowID != right.Target.WindowID {
		return left.Target.WindowID < right.Target.WindowID
	}
	if left.PaneIndex != right.PaneIndex {
		return left.PaneIndex < right.PaneIndex
	}
	return left.Target.PaneID < right.Target.PaneID
}

func appendPathSearchFields(fields []list.SearchField, rawPath, homeDir string) []list.SearchField {
	if rawPath != "" {
		fields = append(fields, list.SearchField{Tier: list.SearchSecondary, Text: rawPath})
	}
	display := displayPath(rawPath, homeDir)
	if display != "" && display != rawPath {
		fields = append(fields, list.SearchField{Tier: list.SearchSecondary, Text: display})
	}
	return fields
}

func destinationItemID(sessionID, windowID, paneID string) list.ItemID {
	return list.ItemID{Scope: sessionID, Parent: windowID, ID: paneID}
}

func paneProgramLabel(pane backend.Pane, defaultShell string) string {
	if pane.Dead {
		return "exited"
	}

	if pane.Program != "" {
		return pane.Program
	}

	if shouldHideCommand(pane.Command, defaultShell) {
		return ""
	}

	if pane.Command != "" {
		return pane.Command
	}

	return "unknown"
}

func shouldHideCommand(command, defaultShell string) bool {
	return command != "" && defaultShell != "" && filepath.Base(command) == filepath.Base(defaultShell)
}
