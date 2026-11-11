package tui

import (
	"context"
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/app/actions"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// OpenActions runs a standalone action picker using the ordinary theme, keymap
// and sizing preferences. It does not open a status subscription or daemon.
func OpenActions(ctx context.Context, subject actions.Subject, path string, overrides Overrides) error {
	if subject != (actions.Subject{}) {
		if err := subject.Validate(); err != nil {
			return err
		}
	}
	if err := overrides.Validate(); err != nil {
		return err
	}
	catalog, err := loadActions(ctx, path)
	if err != nil {
		return &diagnosticError{cause: err}
	}
	inventory, err := tmux.NewBackend()
	if err != nil {
		return err
	}
	flow, err := actions.New(ctx, catalog, inventory, subject)
	if err != nil {
		return &diagnosticError{cause: err}
	}
	preferences, loadErr := loadConfig(ctx)
	options, warning := resolveConfig(preferences)
	options = overrides.apply(options)
	if loadErr != nil {
		warning = loadErr.Error()
	}
	return runActionFlow(ctx, flow, options, configNotice(warning), ui.Run)
}

func runActionFlow(ctx context.Context, flow *actions.Flow, options ui.Options, notice string, run RunUIFunc) error {
	initial := projectActionScreen(flow.Screen())
	initial.Snapshot.Notice = notice
	options.Choice = initial.Choice
	options.Initial = ui.State{View: ui.ViewFlat, Scope: ui.ScopeAll}
	effectsCtx, cancel := context.WithCancel(ctx)
	effects := uiEffects{ctx: effectsCtx, cancel: cancel, execute: func(ctx context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
		return executeActionFlow(ctx, flow, request)
	}}
	defer effects.stop()
	_, err := run(ctx, *initial.Snapshot, options, ui.Runtime{Dispatch: effects.dispatch})
	if isCleanTUIExit(err) {
		return nil
	}
	return err
}

func loadActions(ctx context.Context, path string) (actions.Catalog, error) {
	if path == "" {
		var err error
		path, err = actions.DefaultPath()
		if err != nil {
			return actions.Catalog{}, err
		}
	}
	return actions.Load(ctx, path)
}

func executeActionFlow(ctx context.Context, flow *actions.Flow, request ui.ActionRequest) (ui.ActionResult, error) {
	var screen actions.Screen
	var err error
	switch request.Choice {
	case ui.ChoiceSelect:
		if request.ItemID.Scope != "actions" || request.ItemID.Parent != flow.Screen().Key {
			return projectActionScreen(flow.Screen()), fmt.Errorf("selection belongs to an earlier action screen")
		}
		screen, err = flow.Select(ctx, request.ItemID.ID)
	case ui.ChoiceBack:
		screen, err = flow.Back(ctx)
	case ui.ChoiceRefresh:
		screen, err = flow.Refresh(ctx)
	default:
		return ui.ActionResult{}, fmt.Errorf("unknown action picker event %q", request.Choice)
	}
	result := projectActionScreen(screen)
	if err != nil {
		return result, &diagnosticError{cause: err}
	}
	return result, nil
}

func projectActionScreen(screen actions.Screen) ui.ActionResult {
	snapshot := list.Snapshot{Items: make([]list.Item, 0, len(screen.Items))}
	for _, item := range screen.Items {
		label := terminal.Sanitize(item.Label)
		if label == "" {
			label = "(unnamed)"
		}
		snapshot.Items = append(snapshot.Items, list.Item{
			ID:      list.ItemID{Scope: "actions", Parent: screen.Key, ID: item.ID},
			Primary: label, Secondary: terminal.Sanitize(item.Description),
			SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: label}},
		})
	}
	return ui.ActionResult{
		Snapshot: &snapshot,
		Choice: &ui.ChoiceScreen{
			Key: screen.Key, Title: terminal.Sanitize(screen.Title), Closed: screen.Done,
			ResetHistory: screen.Key == "actions",
			EmptyText:    terminal.Sanitize(screen.EmptyText),
		},
	}
}

func (a *LiveAdapter) openActions(ctx context.Context, item liveItem) (ui.ActionResult, error) {
	catalog, err := loadActions(ctx, "")
	if err != nil {
		return ui.ActionResult{}, &diagnosticError{cause: err}
	}
	b, err := a.detectBackend()
	if err != nil {
		return ui.ActionResult{}, err
	}
	inventory, ok := b.(actions.Inventory)
	if !ok {
		return ui.ActionResult{}, fmt.Errorf("backend does not support action inventory")
	}
	subject := actions.Subject{Kind: "session", SessionID: item.Target.SessionID, WindowID: item.Target.WindowID, PaneID: item.Target.PaneID}
	if subject.PaneID != "" {
		subject.Kind = "pane"
	} else if subject.WindowID != "" {
		subject.Kind = "window"
	}
	flow, err := actions.New(ctx, catalog, inventory, subject)
	if err != nil {
		return ui.ActionResult{}, err
	}
	a.actionFlow = flow
	return projectActionScreen(flow.Screen()), nil
}

func (a *LiveAdapter) executeFlow(ctx context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
	if a.actionFlow == nil {
		return ui.ActionResult{}, fmt.Errorf("action picker is not open")
	}
	result, err := executeActionFlow(ctx, a.actionFlow, request)
	if err != nil || !result.Choice.Closed {
		return result, err
	}
	a.actionFlow = nil
	snapshot, err := a.loadSnapshot(ctx)
	if err != nil {
		result.Snapshot = nil
		return result, err
	}
	result.Snapshot = &snapshot
	return result, nil
}
