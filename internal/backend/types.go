package backend

import "github.com/MSmaili/hetki/internal/status"

type StateResult struct {
	Sessions []Session
	Active   ActiveContext
}

type Session struct {
	ID            string
	Name          string
	WorkspacePath string
	Last          bool
	Windows       []Window
}

type Window struct {
	ID     string
	Name   string
	Index  int
	Path   string
	Layout string
	Active bool
	Panes  []Pane
}

type Pane struct {
	ID      string
	Index   int
	Path    string
	Command string
	// Program is an optional foreground naming hint; Command remains the tmux fact.
	Program   string
	Dead      bool
	Zoom      bool
	Active    bool
	StatusRef status.PaneRef // Optional validated identity; zero means unavailable.
}

type ActiveContext struct {
	SessionID   string
	Session     string
	WindowID    string
	Window      string
	WindowIndex int
	PaneID      string
	Pane        int
	Path        string
}
