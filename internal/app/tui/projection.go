package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/frecency"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

func (a *LiveAdapter) Load(ctx context.Context, view ui.ViewMode) (list.Snapshot, error) {
	if err := view.Validate(); err != nil {
		return list.Snapshot{}, err
	}
	prepared, err := a.loadProjection(ctx, view)
	if err != nil {
		return list.Snapshot{}, err
	}

	if err := a.publishSnapshot(ctx, prepared); err != nil {
		return list.Snapshot{}, err
	}
	return prepared.snapshot, nil
}

func (a *LiveAdapter) loadSnapshot(ctx context.Context) (list.Snapshot, error) {
	a.mu.RLock()
	view := a.projection
	a.mu.RUnlock()
	return a.Load(ctx, view)
}

type projectionInput struct {
	state  liveStateResult
	scores frecency.Scores
	notice string
}

func newProjectionInput(state liveStateResult, scores frecency.Scores, notice string) *projectionInput {
	state.Sessions = slices.Clone(state.Sessions)
	for i := range state.Sessions {
		session := &state.Sessions[i]
		session.Windows = slices.Clone(session.Windows)
		for j := range session.Windows {
			window := &session.Windows[j]
			window.Panes = slices.Clone(window.Panes)
		}
	}
	return &projectionInput{state: state, scores: scores, notice: notice}
}

func (input *projectionInput) project(projection ui.ViewMode) (preparedSnapshot, error) {
	snapshot, index, err := projectState(input.state.StateResult, projection, input.scores, input.notice)
	if err != nil {
		return preparedSnapshot{}, err
	}
	return preparedSnapshot{snapshot: snapshot, index: index, projection: projection, input: input}, nil
}

type preparedSnapshot struct {
	snapshot   list.Snapshot
	index      itemIndex
	projection ui.ViewMode
	input      *projectionInput
}

func (a *LiveAdapter) loadProjection(ctx context.Context, projection ui.ViewMode) (preparedSnapshot, error) {
	result, err := a.queryState(ctx)
	if err != nil {
		return preparedSnapshot{}, err
	}

	scores, notice, err := a.loadFrecency(ctx)
	if err != nil {
		return preparedSnapshot{}, err
	}

	input := newProjectionInput(result, scores, notice)
	return input.project(projection)
}

func (a *LiveAdapter) publishSnapshot(ctx context.Context, prepared preparedSnapshot) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	a.projection = prepared.projection
	a.index = prepared.index
	a.input = prepared.input
	a.statusServer, a.statusErr = prepared.input.state.server, prepared.input.state.statusErr
	return nil
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

func projectState(result backend.StateResult, projection ui.ViewMode, scores frecency.Scores, notice string) (list.Snapshot, itemIndex, error) {
	homeDir, _ := os.UserHomeDir()
	var snapshot list.Snapshot
	var index itemIndex
	var err error
	if projection == ui.ViewFlat {
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
	if !selectedID.IsZero() {
		var err error
		selected, err = a.resolveItem(selectedID)
		if err != nil {
			return ui.ActionResult{}, err
		}
	}
	a.mu.RLock()
	current, input := a.projection, a.input
	a.mu.RUnlock()
	if input == nil {
		return ui.ActionResult{}, fmt.Errorf("live state is not loaded")
	}

	next := ui.ViewFlat
	message := "showing panes"
	if current == ui.ViewFlat {
		next = ui.ViewTree
		message = "showing session tree"
	}

	prepared, err := input.project(next)
	if err != nil {
		return ui.ActionResult{}, err
	}
	preferred := preferredProjectionItem(selected, next, prepared.snapshot, prepared.index)

	if err := a.publishSnapshot(ctx, prepared); err != nil {
		return ui.ActionResult{}, err
	}
	snapshot := prepared.snapshot
	return ui.ActionResult{Message: message, Snapshot: &snapshot, SelectItemID: preferred, View: next}, nil
}

func preferredProjectionItem(
	selected liveItem,
	projection ui.ViewMode,
	snapshot list.Snapshot,
	index itemIndex,
) list.ItemID {
	if projection == ui.ViewTree {
		return preferredTreeItem(selected, snapshot, index)
	}
	return preferredFlatItem(selected, snapshot, index)
}

func preferredTreeItem(selected liveItem, snapshot list.Snapshot, index itemIndex) list.ItemID {
	_, windowExists := index[selected.windowRowID()]
	if !selected.windowRowID().IsZero() && windowExists {
		return selected.windowRowID()
	}
	_, sessionExists := index[selected.sessionRowID()]
	if !selected.sessionRowID().IsZero() && sessionExists {
		return selected.sessionRowID()
	}
	return projectionFallback(snapshot, index)
}

func preferredFlatItem(selected liveItem, snapshot list.Snapshot, index itemIndex) list.ItemID {
	var preferred list.ItemID
	if !selected.windowRowID().IsZero() {
		preferred = firstMatchingItem(snapshot, index, func(item liveItem) bool {
			return item.windowRowID() == selected.windowRowID() && item.PaneActive
		})
	} else if !selected.sessionRowID().IsZero() {
		preferred = firstMatchingItem(snapshot, index, func(item liveItem) bool {
			return item.sessionRowID() == selected.sessionRowID() && item.WindowActive && item.PaneActive
		})
	}
	if !preferred.IsZero() {
		return preferred
	}
	return projectionFallback(snapshot, index)
}

func projectionFallback(snapshot list.Snapshot, index itemIndex) list.ItemID {
	if !snapshot.ActiveItemID.IsZero() {
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
			if id := visit(item.Children); !id.IsZero() {
				return id
			}
		}
		return list.ItemID{}
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
