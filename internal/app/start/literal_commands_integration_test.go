//go:build integration

package start

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestStartupCommandsPreserveEscapesAndRunOnce(t *testing.T) {
	b := newIsolatedStartBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	directory := t.TempDir()
	windowMarker := filepath.Join(directory, "window-startup")
	paneMarker := filepath.Join(directory, "pane-startup")
	commandTemplate := `record() { printf '%%s\n' "$1" >> %q; tmux wait-for -S %s; }; record %s`
	windowCommand := fmt.Sprintf(commandTemplate, windowMarker, "window-started", `window\;`)
	paneCommand := fmt.Sprintf(commandTemplate, paneMarker, "pane-started", `pane\\\;`)
	raw := &manifest.Workspace{Sessions: []manifest.Session{{
		Name: "dev",
		Windows: []manifest.Window{{
			Name: "editor", Path: directory, Command: windowCommand,
			Panes: []manifest.Pane{{}, {Command: paneCommand}},
		}},
	}}}
	source, err := yaml.Marshal(raw)
	require.NoError(t, err)
	workspacePath := filepath.Join(directory, "workspace.yaml")
	require.NoError(t, os.WriteFile(workspacePath, source, 0600))
	service := NewService(func() (Backend, error) { return b, nil })

	err = service.Run(ctx, Options{Workspace: workspacePath})

	require.NoError(t, err)
	for _, channel := range []string{"window-started", "pane-started"} {
		output, err := exec.CommandContext(ctx, "tmux", "wait-for", channel).CombinedOutput()
		require.NoError(t, err, "startup command did not finish: %s", output)
	}
	created, err := b.QueryState(ctx)
	require.NoError(t, err)
	session := findBackendSession(t, created.Sessions, "dev")
	window := findBackendWindow(t, session.Windows, "editor")
	require.Len(t, window.Panes, 2)

	for _, force := range []bool{false, true} {
		workspace, err := manifest.NewFileLoader(workspacePath).Load()
		require.NoError(t, err)
		assert.Equal(t, windowCommand, workspace.Sessions[0].Windows[0].Panes[0].Command)
		assert.Equal(t, paneCommand, workspace.Sessions[0].Windows[0].Panes[1].Command)
		second, err := buildPlan(ctx, b, workspace, force)
		require.NoError(t, err)
		require.True(t, second.IsEmpty(), "matching second start must not repeat commands (force=%v): %v", force, second.Actions)

		require.NoError(t, service.Run(ctx, Options{Workspace: workspacePath, Force: force}))
	}

	after, err := b.QueryState(ctx)
	require.NoError(t, err)
	currentSession := findBackendSession(t, after.Sessions, "dev")
	assert.Equal(t, session.ID, currentSession.ID)
	require.Len(t, currentSession.Windows, 1)
	currentWindow := currentSession.Windows[0]
	assert.Equal(t, window.ID, currentWindow.ID)
	require.Len(t, currentWindow.Panes, 2)
	for i, pane := range currentWindow.Panes {
		assert.Equal(t, window.Panes[i].ID, pane.ID)
	}
	for _, tc := range []struct {
		path string
		want string
	}{
		{path: windowMarker, want: "window;\n"},
		{path: paneMarker, want: "pane\\;\n"},
	} {
		data, err := os.ReadFile(tc.path)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(data), "startup command must preserve shell escapes and run once")
	}
	saved, err := os.ReadFile(workspacePath)
	require.NoError(t, err)
	assert.Equal(t, source, saved, "starting must not rewrite authored commands")
}
