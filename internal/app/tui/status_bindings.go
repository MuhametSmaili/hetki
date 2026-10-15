package tui

import (
	"slices"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/status"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

func (a *LiveAdapter) StatusBindings() ui.StatusBindings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	bindings := make(ui.StatusBindings)
	for id, item := range a.index {
		if len(item.StatusPanes) != 0 {
			bindings[id] = slices.Clone(item.StatusPanes)
		}
	}
	return bindings
}

func paneStatusRefs(panes []backend.Pane) []status.PaneRef {
	var refs []status.PaneRef
	for _, pane := range panes {
		ref := pane.StatusRef
		if ref.Server != "" && ref.ID != "" && ref.ID == pane.ID && ref.Generation != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}
