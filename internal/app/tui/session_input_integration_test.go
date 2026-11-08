//go:build integration

package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestNativeSessionFormPrefillsSelectionAndCreatesInChosenDirectory(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required")
	}
	root := testenv.PrivateHome(t)
	root, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	origin, chosen := filepath.Join(root, "origin"), filepath.Join(root, "chosen path")
	selected := filepath.Join(root, "selected")
	require.NoError(t, os.Mkdir(origin, 0o700))
	require.NoError(t, os.Mkdir(chosen, 0o700))
	require.NoError(t, os.Mkdir(selected, 0o700))
	socket := filepath.Join(root, "tmux.sock")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	run := func(args ...string) string {
		out, err := exec.CommandContext(ctx, "tmux", append([]string{"-S", socket, "-f", "/dev/null"}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	run("new-session", "-d", "-s", "origin", "-c", origin, "/bin/sh", "-c", "exec sleep 60")
	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelCleanup()
		out, err := exec.CommandContext(cleanupCtx, "tmux", "-S", socket, "kill-server").CombinedOutput()
		require.NoError(t, err, "%s", out)
	})
	run("set-option", "-g", "default-shell", "/bin/sh")
	run("new-session", "-d", "-s", "selected", "-c", selected, "/bin/sh", "-c", "exec sleep 60")
	selectedID := run("display-message", "-p", "-t", "selected:", "#{session_id}")
	workspace := filepath.Join(root, "workspace.yaml")
	run("set-option", "-t", "origin", "@hetki_workspace_path", workspace)
	pid := run("display-message", "-p", "#{pid}")
	pane := run("display-message", "-p", "-t", "origin:", "#{pane_id}")
	t.Setenv("TMUX", fmt.Sprintf("%s,%s,0", socket, pid))
	t.Setenv("TMUX_PANE", pane)
	b, err := tmux.NewBackend()
	require.NoError(t, err)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	_, err = adapter.Load(ctx, ui.ViewTree)
	require.NoError(t, err)

	prompt, err := adapter.Execute(ctx, ui.ActionRequest{
		ActionID: ui.ActionCreateSession,
		ItemID:   sessionItemID(selectedID),
	})

	require.NoError(t, err)
	require.Equal(t, selected, prompt.Input.Fields[1].InitialValue)
	require.Equal(t, selected, prompt.Input.Fields[1].BasePath)

	_, err = adapter.Execute(ctx, ui.ActionRequest{
		ActionID: ui.ActionCreateSession,
		Values:   map[string]string{sessionNameField: "invalid", sessionDirectoryField: filepath.Join(root, "missing")},
	})
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, "origin\nselected", run("list-sessions", "-F", "#{session_name}"))
	result, err := adapter.Execute(ctx, ui.ActionRequest{
		ActionID: ui.ActionCreateSession, Values: map[string]string{sessionNameField: "created", sessionDirectoryField: chosen},
	})

	require.NoError(t, err)
	require.NotNil(t, result.Snapshot)
	require.False(t, result.SelectItemID.IsZero())
	require.Empty(t, result.Navigation)
	actual := run("display-message", "-p", "-t", "created:", "#{pane_current_path}|#{@hetki_workspace_path}")
	resolved, err := filepath.EvalSymlinks(chosen)
	require.NoError(t, err)
	require.Equal(t, resolved+"|"+workspace, actual)

	_, err = adapter.Execute(ctx, ui.ActionRequest{
		ActionID: ui.ActionCreateSession, Values: map[string]string{sessionNameField: "created", sessionDirectoryField: chosen},
	})
	require.Error(t, err, "duplicate session must fail without switching or creating another session")
	require.Equal(t, "created\norigin\nselected", run("list-sessions", "-F", "#{session_name}"))
}
