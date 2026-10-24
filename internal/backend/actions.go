package backend

import (
	"errors"
	"fmt"
)

const WorkspacePathOption = "@hetki_workspace_path"

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
	Session  string
	Name     string
	Path     string
	Detached bool
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

type RenameSessionAction struct {
	Current string
	New     string
}

func (a RenameSessionAction) Comment() string {
	return fmt.Sprintf("# Rename session: %s -> %s", a.Current, a.New)
}

func (a RenameSessionAction) Validate() error {
	if a.Current == "" || a.New == "" {
		return errors.New("rename session current and new names cannot be empty")
	}
	return nil
}

type RenameWindowAction struct {
	Session  string
	Window   string
	WindowID string
	New      string
}

func (a RenameWindowAction) Comment() string {
	return fmt.Sprintf("# Rename window: %s:%s -> %s", a.Session, a.Window, a.New)
}

func (a RenameWindowAction) Validate() error {
	if a.Session == "" || a.Window == "" || a.WindowID == "" || a.New == "" {
		return errors.New("rename window session, window, stable ID, and new name cannot be empty")
	}
	return nil
}

type SplitPaneAction struct {
	Session  string
	Window   string
	Path     string
	Detached bool
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
	Name string
}

func (a KillSessionAction) Comment() string {
	return fmt.Sprintf("# Kill session: %s", a.Name)
}

func (a KillSessionAction) Validate() error {
	if a.Name == "" {
		return errors.New("kill session name cannot be empty")
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

type SetSessionOptionAction struct {
	Session string
	Key     string
	Value   string
}

func (a SetSessionOptionAction) Comment() string {
	return fmt.Sprintf("# Set session option: %s %s=%s", a.Session, a.Key, a.Value)
}

func (a SetSessionOptionAction) Validate() error {
	if a.Session == "" || a.Key == "" {
		return errors.New("session option session and key cannot be empty")
	}
	return nil
}
