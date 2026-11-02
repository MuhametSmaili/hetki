//go:build integration

package start

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceKeepsUnrelatedSessionOnIsolatedTmux(t *testing.T) {
	b, navigationLog := newIsolatedStartBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	keepPath, removePath := t.TempDir(), t.TempDir()
	require.NoError(t, b.Apply(ctx, []backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor", Path: t.TempDir()},
		backend.CreateWindowAction{Session: "dev", Name: "scratch", Path: keepPath},
		backend.CreateWindowAction{Session: "dev", Name: "scratch", Path: removePath},
		backend.CreateSessionAction{Name: "personal", WindowName: "shell", Path: t.TempDir()},
	}))

	live, err := b.QueryState(ctx)
	require.NoError(t, err)
	dev := findBackendSession(t, live.Sessions, "dev")
	editor := findBackendWindow(t, dev.Windows, "editor")
	scratch := findBackendWindow(t, dev.Windows, "scratch")
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{
		Name: "dev",
		Windows: []manifest.Window{
			{Name: editor.Name, Path: editor.Path, Layout: editor.Layout, Panes: manifestPanes(editor.Panes)},
			{Name: scratch.Name, Path: scratch.Path, Layout: scratch.Layout, Panes: manifestPanes(scratch.Panes)},
		},
	}}}
	service := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

	err = service.Run(ctx, Options{Path: new("./.hetki.yaml"), Force: true})

	require.NoError(t, err)
	after, err := b.QueryState(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"dev", "personal"}, backendSessionNames(after.Sessions))
	devAfter := findBackendSession(t, after.Sessions, "dev")
	assert.Equal(t, []string{"editor", "scratch"}, backendWindowNames(devAfter.Windows))
	assert.Equal(t, scratch.ID, findBackendWindow(t, devAfter.Windows, "scratch").ID, "force must preserve the matched duplicate by stable ID")
	_, err = os.Stat(navigationLog)
	assert.NoError(t, err)
}

func TestCreationFollowupsUseReturnedIDsOnNonEmptyBaseOneSession(t *testing.T) {
	b, _ := newIsolatedStartBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	require.NoError(t, b.Apply(ctx, []backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor", Path: t.TempDir()},
		backend.CreateSessionAction{Name: "personal", WindowName: "shell", Path: t.TempDir()},
	}))

	live, err := b.QueryState(ctx)
	require.NoError(t, err)
	editor := findBackendWindow(t, findBackendSession(t, live.Sessions, "dev").Windows, "editor")
	serverPath, secondPanePath := t.TempDir(), t.TempDir()
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{
		Name: "dev",
		Windows: []manifest.Window{
			{Name: editor.Name, Path: editor.Path, Layout: editor.Layout, Panes: manifestPanes(editor.Panes)},
			{Name: "server", Path: serverPath, Layout: "tiled", Panes: []manifest.Pane{{Path: serverPath}, {Path: secondPanePath, Zoom: true}}},
		},
	}}}
	service := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

	err = service.Run(ctx, Options{Path: new("./.hetki.yaml")})

	require.NoError(t, err)
	after, err := b.QueryState(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"dev", "personal"}, backendSessionNames(after.Sessions), "unrelated session must survive the force matrix on base index 1")
	dev := findBackendSession(t, after.Sessions, "dev")
	assert.Len(t, findBackendWindow(t, dev.Windows, "editor").Panes, 1, "split must not target the pre-existing window")
	server := findBackendWindow(t, dev.Windows, "server")
	if assert.Len(t, server.Panes, 2) {
		assert.True(t, server.Panes[1].Zoom, "zoom must target the returned pane ID after layout changes")
	}
}

func TestRepeatedStartSkipsUnchangedMetadataOnIsolatedTmux(t *testing.T) {
	b, _ := newIsolatedStartBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	path := filepath.Join(directory, "work.yaml")
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{
		Name: "dev", Windows: []manifest.Window{{Name: "editor", Path: directory}},
	}}}
	service := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) {
		return workspace, path, nil
	}
	require.NoError(t, service.Run(ctx, Options{Path: new(path), Detach: true}))
	live, err := b.QueryState(ctx)
	require.NoError(t, err)
	require.Equal(t, path, findBackendSession(t, live.Sessions, "dev").WorkspacePath)
	t.Setenv("HETKI_TEST_FAIL_COMMAND", "set-option")

	output := captureLoggerOutput(t, func() {
		err = service.Run(ctx, Options{Path: new(path), Detach: true})
	})

	require.NoError(t, err)
	assert.NotContains(t, output, "workspace metadata not set", "unchanged metadata must not attempt the failing mutation")
}

func findBackendSession(t *testing.T, sessions []backend.Session, name string) backend.Session {
	t.Helper()
	for _, session := range sessions {
		if session.Name == name {
			return session
		}
	}
	t.Fatalf("session %q not found", name)
	return backend.Session{}
}

func findBackendWindow(t *testing.T, windows []backend.Window, name string) backend.Window {
	t.Helper()
	for _, window := range windows {
		if window.Name == name {
			return window
		}
	}
	t.Fatalf("window %q not found", name)
	return backend.Window{}
}

func manifestPanes(panes []backend.Pane) []manifest.Pane {
	result := make([]manifest.Pane, len(panes))
	for i, pane := range panes {
		result[i] = manifest.Pane{Path: pane.Path, Command: pane.Command, Zoom: pane.Zoom}
	}
	return result
}

func backendSessionNames(sessions []backend.Session) []string {
	result := make([]string, len(sessions))
	for i, session := range sessions {
		result[i] = session.Name
	}
	return result
}

func backendWindowNames(windows []backend.Window) []string {
	result := make([]string, len(windows))
	for i, window := range windows {
		result[i] = window.Name
	}
	return result
}
