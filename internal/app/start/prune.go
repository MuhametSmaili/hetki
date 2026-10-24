package start

import (
	"errors"
	"fmt"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/plan"
)

// ErrUnsafePruning means session pruning was refused before applying the plan.
var ErrUnsafePruning = errors.New("unsafe session pruning")

// ErrLinkedWindow means reconciliation would destroy a window shared across sessions.
var ErrLinkedWindow = errors.New("linked window")

// Removal session names must come from the same snapshot as sessions.
func validateLinkedWindowRemoval(sessions []backend.Session, p *plan.Plan) error {
	var removalSessions map[string]string
	for _, action := range p.Actions {
		removal, ok := action.(plan.KillWindowAction)
		if !ok {
			continue
		}
		if removalSessions == nil {
			removalSessions = make(map[string]string)
		}
		removalSessions[removal.WindowID] = removal.Session
	}
	if len(removalSessions) == 0 {
		return nil
	}

	for _, session := range sessions {
		for _, window := range session.Windows {
			removalSession, found := removalSessions[window.ID]
			if found && session.Name != removalSession {
				return appshared.WithHint(
					fmt.Errorf("%w: cannot remove or replace window %q (%s) in session %q; it is shared across sessions",
						ErrLinkedWindow, window.Name, window.ID, removalSession),
					"No changes were applied. Resolve the window links before retrying, or run without --force to keep existing windows.")
			}
		}
	}
	return nil
}

func validatePruning(result backend.StateResult, p *plan.Plan) error {
	invoking, err := invokingResources(result)
	if err != nil {
		return err
	}

	for _, action := range p.Actions {
		switch action := action.(type) {
		case plan.KillSessionAction:
			if invoking.sessionIDs[action.SessionID] {
				return appshared.WithHint(
					fmt.Errorf("%w: session %q (%s) contains this invocation", ErrUnsafePruning, action.Name, action.SessionID),
					"Run from a retained session or outside tmux before pruning this session.")
			}
		case plan.KillWindowAction:
			if invoking.windowIDs[action.WindowID] {
				return appshared.WithHint(
					fmt.Errorf("%w: window %q (%s) may contain this invocation", ErrUnsafePruning, action.Window, action.WindowID),
					"Run from a retained pane or outside tmux before replacing or removing this window.")
			}
		}
	}
	return nil
}

type invocationTargets struct {
	sessionIDs map[string]bool
	windowIDs  map[string]bool
}

func invokingResources(result backend.StateResult) (invocationTargets, error) {
	var invoking invocationTargets
	if result.Invocation == nil {
		return invoking, nil
	}

	invoking.sessionIDs = make(map[string]bool)
	invoking.windowIDs = make(map[string]bool)
	for _, session := range result.Sessions {
		for _, window := range session.Windows {
			if windowContainsInvocation(*result.Invocation, session.ID, window) {
				invoking.sessionIDs[session.ID] = true
				invoking.windowIDs[window.ID] = true
			}
		}
	}
	if len(invoking.sessionIDs) == 0 {
		return invoking, appshared.WithHint(
			fmt.Errorf("%w: cannot identify the invoking session", ErrUnsafePruning),
			"Run from a known retained session or outside tmux.")
	}
	return invoking, nil
}

func windowContainsInvocation(identity backend.Invocation, sessionID string, window backend.Window) bool {
	// Current pane membership takes precedence over the inherited session ID.
	// Without a pane ID, conservatively protect every window in that session.
	if identity.PaneID == "" {
		return identity.SessionID != "" && sessionID == identity.SessionID
	}

	for _, pane := range window.Panes {
		if pane.ID == identity.PaneID {
			return true
		}
	}
	return false
}
