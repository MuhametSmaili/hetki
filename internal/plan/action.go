package plan

import (
	"errors"
	"fmt"
)

type Action interface {
	Comment() string
	Validate() error
}

type CreateSessionAction struct {
	Name       string
	WindowName string
	Path       string
}

func (a CreateSessionAction) Comment() string {
	return fmt.Sprintf("# Create session: %s", a.Name)
}

func (a CreateSessionAction) Validate() error {
	if a.Name == "" {
		return errors.New("session name cannot be empty")
	}
	return nil
}

type CreateWindowAction struct {
	Session string
	Name    string
	Path    string
}

func (a CreateWindowAction) Comment() string {
	return fmt.Sprintf("# Create window: %s:%s", a.Session, a.Name)
}

func (a CreateWindowAction) Validate() error {
	if a.Session == "" || a.Name == "" {
		return errors.New("window session and name cannot be empty")
	}
	return nil
}

type SplitPaneAction struct {
	Session string
	Window  string
	Path    string
}

func (a SplitPaneAction) Comment() string {
	return fmt.Sprintf("# Split pane in: %s:%s", a.Session, a.Window)
}

func (a SplitPaneAction) Validate() error {
	if a.Session == "" || a.Window == "" {
		return errors.New("split pane session and window cannot be empty")
	}
	return nil
}

type SendKeysAction struct {
	Session string
	Window  string
	Pane    int
	Command string
}

func (a SendKeysAction) Comment() string {
	return fmt.Sprintf("# Send command to: %s:%s", a.Session, a.Window)
}

func (a SendKeysAction) Validate() error {
	if a.Session == "" || a.Window == "" {
		return errors.New("send keys session and window cannot be empty")
	}
	if a.Pane < 0 {
		return errors.New("send keys pane cannot be negative")
	}
	return nil
}

type KillSessionAction struct {
	Name      string
	SessionID string
}

func (a KillSessionAction) Comment() string {
	return fmt.Sprintf("# Kill session: %q (%s)", a.Name, a.SessionID)
}

func (a KillSessionAction) Validate() error {
	if a.Name == "" || a.SessionID == "" {
		return errors.New("kill session name and stable ID cannot be empty")
	}
	return nil
}

type KillWindowAction struct {
	Session  string
	Window   string
	WindowID string
}

func (a KillWindowAction) Comment() string {
	return fmt.Sprintf("# Kill window: %s:%s", a.Session, a.Window)
}

func (a KillWindowAction) Validate() error {
	if a.Session == "" || a.Window == "" || a.WindowID == "" {
		return errors.New("kill window session, window, and stable ID cannot be empty")
	}
	return nil
}

type SelectLayoutAction struct {
	Session string
	Window  string
	Layout  string
}

func (a SelectLayoutAction) Comment() string {
	return fmt.Sprintf("# Set layout: %s:%s -> %s", a.Session, a.Window, a.Layout)
}

func (a SelectLayoutAction) Validate() error {
	if a.Session == "" || a.Window == "" || a.Layout == "" {
		return errors.New("select layout session, window, and layout cannot be empty")
	}
	return nil
}

type ZoomPaneAction struct {
	Session string
	Window  string
	Pane    int
}

func (a ZoomPaneAction) Comment() string {
	return fmt.Sprintf("# Zoom pane: %s:%s", a.Session, a.Window)
}

func (a ZoomPaneAction) Validate() error {
	if a.Session == "" || a.Window == "" {
		return errors.New("zoom pane session and window cannot be empty")
	}
	if a.Pane < 0 {
		return errors.New("zoom pane cannot be negative")
	}
	return nil
}
