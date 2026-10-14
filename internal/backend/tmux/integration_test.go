//go:build integration

package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIsolatedTmuxBackend runs the client against a throwaway socket; the
// user's default server is never touched.
func newIsolatedTmuxBackend(t *testing.T) *TmuxBackend {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is required for integration tests")
	}
	socketDir, err := os.MkdirTemp("/tmp", "hetki-tmux-parse-")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(socketDir); err != nil {
			t.Errorf("remove isolated tmux directory: %v", err)
		}
	})
	socket := filepath.Join(socketDir, "tmux.sock")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		output, err := exec.CommandContext(ctx, realTmux, "-S", socket, "kill-server").CombinedOutput()
		if err != nil {
			t.Errorf("clean up isolated tmux server: %v: %s", err, output)
		}
	})

	wrapperDir := t.TempDir()
	wrapper := fmt.Sprintf("#!/bin/sh\nexec %q -f /dev/null -S %q \"$@\"\n", realTmux, socket)
	require.NoError(t, os.WriteFile(filepath.Join(wrapperDir, "tmux"), []byte(wrapper), 0755))
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	b, err := NewBackend()
	require.NoError(t, err)
	return b
}

func TestQueryStateRoundTripsDelimiterAndControlValues(t *testing.T) {
	b := newIsolatedTmuxBackend(t)

	sessionName := "dev|ops"
	// '|' previously corrupted the row; backslash names get extra tmux
	// escaping so they stay in unit tests and the option value below.
	windowName := `edit|"main" 'aux' 日本語`
	require.NoError(t, b.Apply(context.Background(), []backend.Action{backend.CreateSessionAction{Name: sessionName, WindowName: windowName}}))
	require.NoError(t, b.client.Execute(context.Background(), SetSessionOption{Session: sessionName, Key: backend.WorkspacePathOption, Value: ` /tmp/ws|path\\1.yaml `}))

	live, err := b.QueryState(context.Background())
	require.NoError(t, err)

	if assert.Len(t, live.Sessions, 1) && assert.Len(t, live.Sessions[0].Windows, 1) {
		assert.Equal(t, sessionName, live.Sessions[0].Name)
		assert.Equal(t, windowName, live.Sessions[0].Windows[0].Name)
		assert.Equal(t, ` /tmp/ws|path\\1.yaml `, live.Sessions[0].WorkspacePath, "workspace path must round-trip byte-exact")
	}
}

func TestQueryStateReadsEffectiveSessionShells(t *testing.T) {
	b := newIsolatedTmuxBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	require.NoError(t, b.Apply(ctx, []backend.Action{
		backend.CreateSessionAction{Name: "inherited", WindowName: "main"},
		backend.CreateSessionAction{Name: "overridden", WindowName: "main"},
	}))
	shell := filepath.Join(t.TempDir(), "shell | override")
	require.NoError(t, os.Symlink("/bin/sh", shell))
	_, err := b.client.Run(ctx,
		"set-option", "-g", "default-shell", "/bin/sh", ";",
		"set-option", "-t", "overridden", "default-shell", shell, ";",
		"link-window", "-s", "inherited:main", "-t", "overridden:")
	require.NoError(t, err)

	state, err := b.QueryState(ctx)

	require.NoError(t, err)
	require.Len(t, state.Sessions, 2)
	require.Equal(t, "/bin/sh", state.Sessions[0].DefaultShell)
	require.Equal(t, shell, state.Sessions[1].DefaultShell)
	require.Len(t, state.Sessions[1].Windows, 2)
}

func TestQueryStateFailsClosedOnNewlineValues(t *testing.T) {
	for _, value := range []string{"x\n", "a\nb", "\n", "a\n\nb"} {
		b := newIsolatedTmuxBackend(t)
		require.NoError(t, b.Apply(context.Background(), []backend.Action{backend.CreateSessionAction{Name: "dev", WindowName: "ed"}}))
		require.NoError(t, b.client.Execute(context.Background(), SetSessionOption{Session: "dev", Key: backend.WorkspacePathOption, Value: value}))

		_, err := b.QueryState(context.Background())
		assert.Error(t, err, "workspace option %q must fail closed", value)
	}
}
