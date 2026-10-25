package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
)

// ResolveWorkspaceTarget requires exactly one selector. A nil pointer means
// omitted; a pointer to an empty string is an explicitly empty selector.
func ResolveWorkspaceTarget(name, path *string) (string, error) {
	if name == nil && path == nil {
		return "", fmt.Errorf("choose exactly one workspace target: NAME or --file FILE")
	}
	if name != nil && path != nil {
		return "", fmt.Errorf("cannot combine a workspace name with --file")
	}

	if name != nil {
		if *name == "" {
			return "", fmt.Errorf("workspace name must not be empty")
		}
		return manifest.NewResolver().NamedPath(*name)
	}
	if *path == "" {
		return "", fmt.Errorf("--file must not be empty")
	}
	return manifest.FilePath(*path)
}

// WorkspaceFromSessions projects observations using the backend's pure preset
// conversion. Single-pane layouts remain omitted.
func WorkspaceFromSessions(sessions []backend.Session, layoutPreset func(string) (string, error)) (*manifest.Workspace, error) {
	ws := &manifest.Workspace{Sessions: make([]manifest.Session, len(sessions))}

	for i, sess := range sessions {
		windows, err := windowsFromBackend(sess.Windows, layoutPreset)
		if err != nil {
			return nil, fmt.Errorf("session %q: %w", sess.Name, err)
		}

		ws.Sessions[i] = manifest.Session{
			Name:    sess.Name,
			Windows: windows,
		}
	}

	return ws, nil
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

func windowsFromBackend(windows []backend.Window, layoutPreset func(string) (string, error)) ([]manifest.Window, error) {
	result := make([]manifest.Window, len(windows))
	for i, w := range windows {
		result[i] = manifest.Window{
			Name: w.Name,
			Path: contractHomePath(w.Path),
		}
		if len(w.Panes) > 1 {
			layout, err := layoutPreset(w.Layout)
			if err != nil {
				return nil, fmt.Errorf("window %q: %w", w.Name, err)
			}

			result[i].Layout = layout
			result[i].Panes = panesFromBackend(w.Panes)
		}
	}
	return result, nil
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
