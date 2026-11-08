package actions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/MuhametSmaili/hetki/internal/backend"
)

// Source is the consumer-owned seam shared by native and executable sources.
type Source interface {
	List(context.Context, SourceSpec, Request) ([]Item, error)
}

type executableSource struct{}

func (executableSource) List(ctx context.Context, spec SourceSpec, request Request) ([]Item, error) {
	result, err := runCommand(ctx, *spec.Exec, request)
	if err != nil {
		return nil, err
	}
	return decodeItems(result.output)
}

func decodeItems(output []byte) ([]Item, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), maxRecord)
	var items []Item
	for scanner.Scan() {
		if len(items) == MaxItems {
			return nil, fmt.Errorf("source exceeds %d items", MaxItems)
		}
		var item Item
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		decoder.UseNumber()
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("source item %d: %w", len(items)+1, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("source item %d has trailing data", len(items)+1)
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read source records (limit %d bytes each): %w", maxRecord, err)
	}
	return items, validateItems(items)
}

type inventorySource struct {
	sessions []backend.Session
}

func (s inventorySource) List(ctx context.Context, spec SourceSpec, request Request) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sessionID, err := resolveText(spec.Session, request, maxExpandedText)
	if err != nil {
		return nil, err
	}
	windowID, err := resolveText(spec.Window, request, maxExpandedText)
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, session := range s.sessions {
		if sessionID != "" && session.ID != sessionID {
			continue
		}
		items = append(items, sessionItems(session, spec.Builtin, windowID)...)
	}
	return items, validateItems(items)
}

func sessionItems(session backend.Session, source, windowID string) []Item {
	if source == "tmux.sessions" {
		target := backend.Target{SessionID: session.ID}
		return []Item{{ID: session.ID, Label: session.Name, Value: targetValue(target), target: &target}}
	}
	var items []Item
	for _, window := range session.Windows {
		if windowID != "" && window.ID != windowID {
			continue
		}
		items = append(items, windowItems(session, window, source)...)
	}
	return items
}

func windowItems(session backend.Session, window backend.Window, source string) []Item {
	var items []Item
	for _, pane := range window.Panes {
		if source == "tmux.windows" && !pane.Active {
			continue
		}
		target := backend.Target{SessionID: session.ID, WindowID: window.ID, PaneID: pane.ID}
		id := session.ID + ":" + window.ID
		label := fmt.Sprintf("%s / %d %s", session.Name, window.Index, window.Name)
		if source == "tmux.panes" {
			id += "." + pane.ID
			label += fmt.Sprintf(" / %d %s", pane.Index, pane.Command)
		}
		items = append(items, Item{ID: id, Label: label, Description: pane.Path, Value: targetValue(target), target: &target})
	}
	return items
}

func targetValue(target backend.Target) map[string]any {
	value := map[string]any{"session_id": target.SessionID}
	if target.WindowID != "" {
		value["window_id"] = target.WindowID
		value["window_target"] = target.SessionID + ":" + target.WindowID
	}
	if target.PaneID != "" {
		value["pane_id"] = target.PaneID
		value["pane_target"] = target.SessionID + ":" + target.WindowID + "." + target.PaneID
	}
	return value
}
