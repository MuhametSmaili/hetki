package backend

type StateResult struct {
	Sessions []Session
	Active   ActiveContext
	// Invocation is nil outside tmux. A non-nil value with unresolved IDs is
	// unknown, not permission to delete the process's containing session.
	Invocation *Invocation
}

// Invocation identifies the environment in which the caller runs. A pane may
// have moved or be linked into several sessions; consumers must check membership.
type Invocation struct {
	SessionID string
	PaneID    string
}

type Session struct {
	ID            string
	Name          string
	WorkspacePath string
	DefaultShell  string
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
	Program string
	Dead    bool
	Zoom    bool
	Active  bool
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
