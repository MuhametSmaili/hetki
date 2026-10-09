package status

import (
	"fmt"
	"iter"
	"maps"
	"time"
)

const MaxPanes = 1024

// Snapshot is immutable; its zero value is empty.
type Snapshot struct {
	panes map[PaneRef]Value
}

func NewSnapshot(values map[PaneRef]Value) (Snapshot, error) {
	if len(values) > MaxPanes {
		return Snapshot{}, fmt.Errorf("status snapshot exceeds %d panes", MaxPanes)
	}

	for pane, value := range values {
		if pane.Server == "" || pane.ID == "" || pane.Generation == "" {
			return Snapshot{}, fmt.Errorf("status snapshot requires server, pane and generation")
		}

		switch value.State {
		case Withdrawn, Unknown, Idle, Working, Blocked:
		default:
			return Snapshot{}, fmt.Errorf("invalid pane status %q", value.State)
		}
	}

	return Snapshot{panes: maps.Clone(values)}, nil
}

func (s Snapshot) At(pane PaneRef, now time.Time) State {
	return s.Lookup(pane).At(now)
}

func (s Snapshot) Lookup(pane PaneRef) Value {
	return s.panes[pane]
}

func (s Snapshot) Len() int {
	return len(s.panes)
}

func (s Snapshot) All() iter.Seq2[PaneRef, Value] {
	return maps.All(s.panes)
}

func (s Snapshot) Invalidate() Snapshot {
	values := make(map[PaneRef]Value, len(s.panes))
	for pane, value := range s.panes {
		if value.State != Withdrawn {
			value = Value{State: Unknown}
		}

		values[pane] = value
	}

	return Snapshot{panes: values}
}
