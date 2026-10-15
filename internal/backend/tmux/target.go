package tmux

import (
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/internal/format"
)

func validateTarget(target backend.Target) error {
	if err := format.ValidateObjectID("session", target.SessionID, '$'); err != nil {
		return err
	}
	if target.PaneID != "" && target.WindowID == "" {
		return fmt.Errorf("pane target requires a window ID")
	}
	if target.WindowID != "" {
		if err := format.ValidateObjectID("window", target.WindowID, '@'); err != nil {
			return err
		}
	}
	if target.PaneID != "" {
		return format.ValidateObjectID("pane", target.PaneID, '%')
	}
	return nil
}

// Linked windows share IDs, so resolve indexes only inside the selected session.
func resolveSwitchTarget(sessions []Session, target backend.Target) (string, error) {
	window, err := findTargetWindow(sessions, target)
	if err != nil {
		return "", err
	}
	resolved := fmt.Sprintf("%s:%d", target.SessionID, window.Index)
	if target.PaneID == "" {
		return resolved, nil
	}
	for _, pane := range window.Panes {
		if pane.ID == target.PaneID {
			return fmt.Sprintf("%s.%d", resolved, pane.Index), nil
		}
	}
	return "", fmt.Errorf("pane %q not found in %s:%s", target.PaneID, target.SessionID, target.WindowID)
}

func findTargetWindow(sessions []Session, target backend.Target) (Window, error) {
	for _, session := range sessions {
		if session.ID != target.SessionID {
			continue
		}
		for _, window := range session.Windows {
			if window.ID == target.WindowID {
				return window, nil
			}
		}
		return Window{}, fmt.Errorf("window %q not found in session %q", target.WindowID, target.SessionID)
	}
	return Window{}, fmt.Errorf("session %q not found", target.SessionID)
}
