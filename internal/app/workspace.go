package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/manifest"
)

type WorkspaceLoader struct {
	Resolve  func(string) (string, error)
	LoadFile func(string) (*manifest.Workspace, error)
}

func NewWorkspaceLoader() WorkspaceLoader {
	resolver := manifest.NewResolver()
	return WorkspaceLoader{
		Resolve: resolver.Resolve,
		LoadFile: func(path string) (*manifest.Workspace, error) {
			return manifest.NewFileLoader(path).Load()
		},
	}
}

func (l WorkspaceLoader) LoadWorkspace(nameOrPath string) (*manifest.Workspace, string, error) {
	if l.Resolve == nil || l.LoadFile == nil {
		defaults := NewWorkspaceLoader()
		if l.Resolve == nil {
			l.Resolve = defaults.Resolve
		}
		if l.LoadFile == nil {
			l.LoadFile = defaults.LoadFile
		}
	}

	workspacePath, err := l.Resolve(nameOrPath)
	if err != nil {
		return nil, "", err
	}

	workspace, err := l.LoadFile(workspacePath)
	if err != nil {
		return nil, "", fmt.Errorf("loading workspace: %w", err)
	}

	return workspace, workspacePath, nil
}

func WorkspaceFromSessions(sessions []backend.Session) *manifest.Workspace {
	ws := &manifest.Workspace{Sessions: make([]manifest.Session, len(sessions))}

	for i, sess := range sessions {
		ws.Sessions[i] = manifest.Session{
			Name:    sess.Name,
			Windows: windowsFromBackend(sess.Windows),
		}
	}

	return ws
}

func MergeWorkspaces(existing, incoming *manifest.Workspace) *manifest.Workspace {
	seen := make(map[string]int, len(existing.Sessions))
	for i, sess := range existing.Sessions {
		seen[sess.Name] = i
	}

	for _, sess := range incoming.Sessions {
		if idx, ok := seen[sess.Name]; ok {
			existing.Sessions[idx] = sess
		} else {
			existing.Sessions = append(existing.Sessions, sess)
		}
	}
	return existing
}

func windowsFromBackend(windows []backend.Window) []manifest.Window {
	result := make([]manifest.Window, len(windows))
	for i, w := range windows {
		result[i] = manifest.Window{
			Name: w.Name,
			Path: contractHomePath(w.Path),
		}
		if len(w.Panes) > 1 {
			result[i].Panes = panesFromBackend(w.Panes)
		}
	}
	return result
}

func panesFromBackend(panes []backend.Pane) []manifest.Pane {
	result := make([]manifest.Pane, len(panes))
	for i, p := range panes {
		result[i] = manifest.Pane{Path: contractHomePath(p.Path), Zoom: p.Zoom}
	}
	return result
}

func contractHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	if path == home {
		return "~"
	}
	return path
}
