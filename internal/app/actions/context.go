package actions

import (
	"context"
	"fmt"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
)

// Inventory reads action context with limits on query time and output size.
// It does not start a server or daemon.
type Inventory interface {
	QueryActionState(context.Context) (tmux.ActionState, error)
}

// Subject keeps optional ancestry absent when a pane/window has multiple links.
type Subject struct {
	Kind      string `json:"kind"`
	SessionID string `json:"session_id,omitempty"`
	WindowID  string `json:"window_id,omitempty"`
	PaneID    string `json:"pane_id,omitempty"`
	// CWD is captured at flow start; empty means unknown.
	CWD string `json:"cwd,omitempty"`
}

func (s Subject) Validate() error {
	var id string
	var prefix byte
	switch s.Kind {
	case "pane":
		id, prefix = s.PaneID, '%'
	case "window":
		id, prefix = s.WindowID, '@'
	case "session":
		id, prefix = s.SessionID, '$'
	default:
		return fmt.Errorf("action subject must be a pane, window or session")
	}
	if len(id) < 2 || id[0] != prefix || strings.Trim(id[1:], "0123456789") != "" {
		return fmt.Errorf("%s requires an exact %c-prefixed tmux ID", s.Kind, prefix)
	}
	return nil
}

func (s Subject) field(name string) any {
	fields := map[string]string{"kind": s.Kind, "session_id": s.SessionID, "window_id": s.WindowID, "pane_id": s.PaneID, "cwd": s.CWD}
	if value := fields[name]; value != "" {
		return value
	}
	return nil
}

type ServerContext struct {
	Socket string `json:"socket"`
}

type Invocation struct {
	SessionID string `json:"session_id,omitempty"`
	PaneID    string `json:"pane_id,omitempty"`
}

type Context struct {
	Server     *ServerContext `json:"server,omitempty"`
	Subject    *Subject       `json:"subject,omitempty"`
	Invocation *Invocation    `json:"invocation,omitempty"`
}

func initialSubject(subject Subject, state backend.StateResult) (*Subject, error) {
	if subject == (Subject{}) {
		if state.Invocation == nil {
			return nil, nil
		}
		if state.Invocation.PaneID == "" {
			return nil, fmt.Errorf("cannot infer the invoking tmux pane; specify --pane, --window or --session")
		}
		// The TMUX variable can contain the old session ID after a pane moves.
		subject = Subject{Kind: "pane", PaneID: state.Invocation.PaneID}
	}
	if err := subject.Validate(); err != nil {
		return nil, err
	}
	resolved, err := resolveSubject(subject, state.Sessions)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// Request is the versioned stdin contract for providers and captured commands.
// Sources must treat all values as immutable for the duration of List.
type Request struct {
	Version int             `json:"version"`
	Context Context         `json:"context"`
	Steps   map[string]Item `json:"steps"`
}

func resolveSubject(subject Subject, sessions []backend.Session) (Subject, error) {
	var matches []Subject
	for _, session := range sessions {
		if subject.SessionID != "" && subject.SessionID != session.ID {
			continue
		}
		matches = append(matches, subjectMatches(subject, session)...)
	}
	if len(matches) == 0 {
		return Subject{}, fmt.Errorf("action subject no longer exists in the selected scope")
	}
	if len(matches) == 1 {
		subject.SessionID = matches[0].SessionID
	}
	subject.WindowID = matches[0].WindowID
	subject.CWD = matches[0].CWD
	return subject, nil
}

func subjectMatches(subject Subject, session backend.Session) []Subject {
	if subject.Kind == "session" {
		return []Subject{{Kind: "session", SessionID: session.ID}}
	}
	var matches []Subject
	for _, window := range session.Windows {
		if subject.WindowID != "" && subject.WindowID != window.ID {
			continue
		}
		if subject.Kind == "window" {
			matches = append(matches, Subject{Kind: "window", SessionID: session.ID, WindowID: window.ID})
			continue
		}
		for _, pane := range window.Panes {
			if pane.ID == subject.PaneID {
				matches = append(matches, Subject{
					Kind: "pane", SessionID: session.ID, WindowID: window.ID, PaneID: pane.ID,
					CWD: pane.Path,
				})
			}
		}
	}
	return matches
}
