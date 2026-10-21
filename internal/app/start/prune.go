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

func validatePruning(result backend.StateResult, p *plan.Plan) error {
	invoking, err := invokingResources(result)
	if err != nil {
		return err
	}

	shared := sharedWindows(result.Sessions)
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
			if shared[action.WindowID] {
				return appshared.WithHint(
					fmt.Errorf("%w: window %q (%s) is linked across sessions", ErrUnsafePruning, action.Window, action.WindowID),
					"Remove --force to prune sessions without replacing windows, or resolve the linked windows first.")
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

func sharedWindows(sessions []backend.Session) map[string]bool {
	owners := make(map[string]string)
	shared := make(map[string]bool)
	for _, session := range sessions {
		for _, window := range session.Windows {
			if owner, found := owners[window.ID]; found && owner != session.ID {
				shared[window.ID] = true
			}
			owners[window.ID] = session.ID
		}
	}
	return shared
}
