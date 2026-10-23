package tui

import (
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type liveItemKind uint8

const (
	liveSession liveItemKind = iota + 1
	liveWindow
	liveDestination
)

type liveItem struct {
	ID           list.ItemID
	Kind         liveItemKind
	Label        string
	Name         string
	SessionName  string
	WindowName   string
	Target       backend.Target
	RawPath      string
	WindowIndex  int
	WindowActive bool
	PaneActive   bool
	PaneIndex    int
	OpenPaneID   string
	Last         bool
}

func (item liveItem) sessionRowID() list.ItemID {
	return sessionItemID(item.Target.SessionID)
}

func (item liveItem) windowRowID() list.ItemID {
	if item.Target.WindowID == "" {
		return list.ItemID{}
	}
	return windowItemID(item.Target.SessionID, item.Target.WindowID)
}

func (item liveItem) parentRowID() list.ItemID {
	if item.Kind != liveWindow {
		return list.ItemID{}
	}
	return item.sessionRowID()
}

type itemIndex map[list.ItemID]liveItem

func validateProjection(snapshot list.Snapshot, index itemIndex) error {
	if err := list.Validate(snapshot); err != nil {
		return err
	}
	ids := list.IDs(snapshot.Items)
	if len(ids) != len(index) {
		return fmt.Errorf("item projection has %d items but index has %d", len(ids), len(index))
	}
	for id := range ids {
		item, exists := index[id]
		if !exists {
			return fmt.Errorf("item %q is missing from owner index", id)
		}
		if item.ID != id {
			return fmt.Errorf("owner index entry %q contains item %q", id, item.ID)
		}
	}
	for id := range index {
		if _, exists := ids[id]; !exists {
			return fmt.Errorf("owner index item %q is missing from projection", id)
		}
	}
	return nil
}
