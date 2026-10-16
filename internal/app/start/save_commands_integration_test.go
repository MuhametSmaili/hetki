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

	appsave "github.com/MuhametSmaili/hetki/internal/app/save"
	"github.com/MuhametSmaili/hetki/internal/backend"
	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/converter"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestSavedCommandsRestoreWithoutReplayingOnMatchingStarts(t *testing.T) {
	b := newIsolatedStartBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, b.Apply(ctx, []backend.Action{
		backend.CreateSessionAction{Name: "anchor", WindowName: "shell", Path: directory},
	}))
	markers := []string{filepath.Join(directory, "first-starts"), filepath.Join(directory, "second-starts")}
	commands := make([]string, len(markers))
	for i, marker := range markers {
		commands[i] = fmt.Sprintf("printf 'started\\n' >> %q; tmux wait-for -S saved-command-%d", marker, i)
	}
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{{
		Name: "editor", Path: directory, Layout: "even-horizontal", Command: commands[0],
		Panes: []manifest.Pane{{}, {Command: commands[1], Zoom: true}},
	}}}}}
	path := filepath.Join(directory, "workspace.yaml")
	data, err := manifest.Marshal(workspace, path, backendtmux.ValidateAuthoredLayout)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	starter := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)
	require.NoError(t, starter.Run(ctx, Options{Path: new(path)}))
	for i := range markers {
		output, err := exec.CommandContext(ctx, "tmux", "wait-for", fmt.Sprintf("saved-command-%d", i)).CombinedOutput()
		require.NoError(t, err, "initial startup: %s", output)
	}
	before, err := b.QueryState(ctx)
	require.NoError(t, err)
	saver := appsave.NewService(func() (appsave.Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)

	_, err = saver.Run(ctx, appsave.Options{Path: new(path), All: true})

	require.NoError(t, err)
	saved, err := manifest.NewFileLoader(path, backendtmux.ValidateAuthoredLayout).Load()
	require.NoError(t, err)
	dev := converter.ManifestToState(saved).Sessions["dev"]
	require.NotNil(t, dev)
	require.Equal(t, commands[0], dev.Windows[0].Panes[0].Command)
	require.Equal(t, commands[1], dev.Windows[0].Panes[1].Command)
	require.True(t, dev.Windows[0].Panes[1].Zoom)

	originalDev := findBackendSession(t, before.Sessions, "dev")
	originalPane := originalDev.Windows[0].Panes[1]
	require.True(t, originalPane.Zoom)

	// Avoid tmux 3.4's zoomed-window teardown crash (tmux/tmux#3857).
	// Only unzoom the disposable instance; the saved workspace remains zoomed.
	output, err := exec.CommandContext(ctx, "tmux", "resize-pane", "-Z", "-t", originalPane.ID).CombinedOutput()
	require.NoError(t, err, "unzoom original window: %s", output)

	require.NoError(t, b.Apply(ctx, []backend.Action{backend.KillSessionAction{Name: "dev"}}))
	require.NoError(t, starter.Run(ctx, Options{Path: new(path)}))
	for i := range markers {
		output, err := exec.CommandContext(ctx, "tmux", "wait-for", fmt.Sprintf("saved-command-%d", i)).CombinedOutput()
		require.NoError(t, err, "restored startup: %s", output)
	}
	restored, err := b.QueryState(ctx)
	require.NoError(t, err)
	restoredDev := findBackendSession(t, restored.Sessions, "dev")
	require.NotEqual(t, originalDev.ID, restoredDev.ID)
	require.True(t, restoredDev.Windows[0].Panes[1].Zoom)

	for _, force := range []bool{false, true} {
		planned, err := buildPlan(ctx, b, saved, force)
		require.NoError(t, err)
		require.True(t, planned.IsEmpty(), "matching start must not recreate panes or resend commands: %v", planned.Actions)
		require.NoError(t, starter.Run(ctx, Options{Path: new(path), Force: force}))
	}
	after, err := b.QueryState(ctx)
	require.NoError(t, err)
	require.Equal(t, restoredDev.ID, findBackendSession(t, after.Sessions, "dev").ID)
	require.Equal(t, restoredDev.Windows[0].ID, findBackendSession(t, after.Sessions, "dev").Windows[0].ID)
	// A shell barrier drains any replayed input before the marker assertions.
	for i, marker := range markers {
		paneID := restoredDev.Windows[0].Panes[i].ID
		channel := fmt.Sprintf("saved-command-drained-%d", i)
		output, err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", paneID, "-l", "tmux wait-for -S "+channel,
			";", "send-keys", "-t", paneID, "Enter").CombinedOutput()
		require.NoError(t, err, "queue shell barrier: %s", output)
		output, err = exec.CommandContext(ctx, "tmux", "wait-for", channel).CombinedOutput()
		require.NoError(t, err, "drain shell input: %s", output)

		data, err := os.ReadFile(marker)
		require.NoError(t, err)
		require.Equal(t, "started\nstarted\n", string(data), "one original launch and one restore, no replay")
	}
}
