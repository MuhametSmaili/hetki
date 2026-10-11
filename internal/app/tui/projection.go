package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/frecency"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
)

type projectionKind uint8

const (
	projectionTree projectionKind = iota
	projectionFlat
)

func (a *LiveAdapter) Load(ctx context.Context) (list.Snapshot, error) {
	return a.loadSnapshot(ctx)
}

func (a *LiveAdapter) loadSnapshot(ctx context.Context) (list.Snapshot, error) {
	result, err := a.queryState(ctx)
	if err != nil {
		return list.Snapshot{}, err
	}
	scores, notice, err := a.loadFrecency(ctx)
	if err != nil {
		return list.Snapshot{}, err
	}
	snapshot, index, err := projectState(result.StateResult, a.projection, scores, notice)
	if err != nil {
		return list.Snapshot{}, err
	}
	a.mu.Lock()
	a.index = index
	a.statusServer, a.statusErr = result.server, result.statusErr
	a.mu.Unlock()
	return snapshot, nil
}

type liveStateResult struct {
	backend.StateResult
	server    observe.Server
	statusErr error
}

func (a *LiveAdapter) queryState(ctx context.Context) (liveStateResult, error) {
	if err := ctx.Err(); err != nil {
		return liveStateResult{}, err
	}
	b, err := a.detectBackend()
	if err != nil {
		return liveStateResult{}, fmt.Errorf("failed to detect backend: %w", err)
	}
	if provider, ok := b.(interface {
		QueryStatusState(context.Context) (backend.StateResult, observe.Server, error)
	}); ok {
		state, server, statusErr := provider.QueryStatusState(ctx)
		if statusErr == nil {
			return liveStateResult{StateResult: state, server: server}, nil
		}
		result, err := b.QueryState(ctx)
		return liveStateResult{StateResult: result, statusErr: statusErr}, err
	}
	result, err := b.QueryState(ctx)
	if err != nil {
		return liveStateResult{}, fmt.Errorf("failed to query sessions: %w", err)
	}
	return liveStateResult{StateResult: result}, nil
}

func (a *LiveAdapter) loadFrecency(ctx context.Context) (frecency.Scores, string, error) {
	if a.frecencyErr != nil {
		return frecency.Scores{}, "frecency unavailable: " + a.frecencyErr.Error(), nil
	}
	if a.frecency == nil {
		return frecency.Scores{}, "", nil
	}
	records, err := a.frecency.LoadContext(ctx)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return frecency.Scores{}, "", err
	}
	if err != nil {
		return frecency.Scores{}, "frecency state: " + err.Error(), nil
	}
	return frecency.NewScores(records, time.Now()), "", nil
}

func projectState(result backend.StateResult, projection projectionKind, scores frecency.Scores, notice string) (list.Snapshot, itemIndex, error) {
	homeDir, _ := os.UserHomeDir()
	var snapshot list.Snapshot
	var index itemIndex
	var err error
	if projection == projectionFlat {
		snapshot, index, err = projectFlatRanked(result, homeDir, scores)
	} else {
		snapshot, index, err = projectTree(result, homeDir)
	}
	if err != nil {
		return list.Snapshot{}, nil, fmt.Errorf("project live sessions: %w", err)
	}
	snapshot.Notice = notice
	return snapshot, index, nil
}

func (a *LiveAdapter) toggleProjection(ctx context.Context, selectedID list.ItemID) (ui.ActionResult, error) {
	var selected liveItem
	if selectedID != "" {
		var err error
		selected, err = a.resolveItem(selectedID)
		if err != nil {
			return ui.ActionResult{}, err
		}
	}
	next := projectionFlat
	message := "showing panes"
	if a.projection == projectionFlat {
		next = projectionTree
		message = "showing session tree"
	}
	state, err := a.queryState(ctx)
	if err != nil {
		return ui.ActionResult{}, err
	}
	scores, notice, err := a.loadFrecency(ctx)
	if err != nil {
		return ui.ActionResult{}, err
	}
	snapshot, index, err := projectState(state.StateResult, next, scores, notice)
	if err != nil {
		return ui.ActionResult{}, err
	}
	preferred := preferredProjectionItem(selected, next, snapshot, index)
	a.projection = next
	a.mu.Lock()
	a.index = index
	a.statusServer, a.statusErr = state.server, state.statusErr
	a.mu.Unlock()
	return ui.ActionResult{Message: message, Snapshot: &snapshot, SelectItemID: preferred}, nil
}

func preferredProjectionItem(
	selected liveItem,
	projection projectionKind,
	snapshot list.Snapshot,
	index itemIndex,
) list.ItemID {
	if projection == projectionTree {
		return preferredTreeItem(selected, snapshot, index)
	}
	return preferredFlatItem(selected, snapshot, index)
}

func preferredTreeItem(selected liveItem, snapshot list.Snapshot, index itemIndex) list.ItemID {
	_, windowExists := index[selected.WindowID]
	if selected.WindowID != "" && windowExists {
		return selected.WindowID
	}
	_, sessionExists := index[selected.SessionID]
	if selected.SessionID != "" && sessionExists {
		return selected.SessionID
	}
	return projectionFallback(snapshot, index)
}

func preferredFlatItem(selected liveItem, snapshot list.Snapshot, index itemIndex) list.ItemID {
	var preferred list.ItemID
	if selected.WindowID != "" {
		preferred = firstMatchingItem(snapshot, index, func(item liveItem) bool {
			return item.WindowID == selected.WindowID && item.PaneActive
		})
	} else if selected.SessionID != "" {
		preferred = firstMatchingItem(snapshot, index, func(item liveItem) bool {
			return item.SessionID == selected.SessionID && item.WindowActive && item.PaneActive
		})
	}
	if preferred != "" {
		return preferred
	}
	return projectionFallback(snapshot, index)
}

func projectionFallback(snapshot list.Snapshot, index itemIndex) list.ItemID {
	if snapshot.ActiveItemID != "" {
		return snapshot.ActiveItemID
	}
	return firstMatchingItem(snapshot, index, func(liveItem) bool { return true })
}

func firstMatchingItem(snapshot list.Snapshot, index itemIndex, matches func(liveItem) bool) list.ItemID {
	var visit func([]list.Item) list.ItemID
	visit = func(items []list.Item) list.ItemID {
		for _, item := range items {
			if live, exists := index[item.ID]; exists && matches(live) {
				return item.ID
			}
			if id := visit(item.Children); id != "" {
				return id
			}
		}
		return ""
	}
	return visit(snapshot.Items)
}

func displayPath(path, home string) string {
	path = strings.TrimSpace(path)
	if path == "" || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}
