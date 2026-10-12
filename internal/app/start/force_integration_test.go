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

	"github.com/MSmaili/hetki/internal/backend"
	backendtmux "github.com/MSmaili/hetki/internal/backend/tmux"
	"github.com/MSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceReplacementPreservesSessionOnIsolatedTmux(t *testing.T) {
	for _, tc := range []struct {
		name       string
		oldWindows []string
		newWindows []string
	}{
		{
			name:       "sole mismatched window",
			oldWindows: []string{"editor"},
			newWindows: []string{"editor"},
		},
		{
			name:       "complete window set replacement",
			oldWindows: []string{"legacy", "scratch"},
			newWindows: []string{"editor", "server"},
		},
		{
			name:       "mismatched window with an extra window",
			oldWindows: []string{"editor", "scratch"},
			newWindows: []string{"editor"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newForceTestBackend(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()

			directory, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			actions := []backend.Action{
				backend.CreateSessionAction{Name: "dev", WindowName: tc.oldWindows[0], Path: directory},
				backend.CreateSessionAction{Name: "personal", WindowName: "shell", Path: directory},
			}
			for _, name := range tc.oldWindows[1:] {
				actions = append(actions, backend.CreateWindowAction{Session: "dev", Name: name, Path: directory})
			}
			require.NoError(t, b.Apply(ctx, actions))
			before, err := b.QueryState(ctx)
			require.NoError(t, err)
			devBefore := findBackendSession(t, before.Sessions, "dev")
			personalBefore := findBackendSession(t, before.Sessions, "personal")
			oldIDs := make([]string, len(devBefore.Windows))
			for i, window := range devBefore.Windows {
				oldIDs[i] = window.ID
			}
			workspace := forceReplacementWorkspace(directory, tc.newWindows)
			service := NewService(func(...string) (backend.Backend, error) { return b, nil })
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

			err = service.Run(ctx, Options{Force: true})

			require.NoError(t, err)
			after, err := b.QueryState(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"dev", "personal"}, backendSessionNames(after.Sessions))
			assert.Equal(t, personalBefore, findBackendSession(t, after.Sessions, "personal"))
			devAfter := findBackendSession(t, after.Sessions, "dev")
			assert.Equal(t, devBefore.ID, devAfter.ID, "the existing session must survive replacement")
			assert.ElementsMatch(t, tc.newWindows, backendWindowNames(devAfter.Windows))
			for _, window := range devAfter.Windows {
				assert.NotContains(t, oldIDs, window.ID)
				require.Len(t, window.Panes, 2)
				assert.Equal(t, directory, window.Panes[0].Path)
				assert.Equal(t, directory, window.Panes[1].Path)
				assert.True(t, window.Panes[1].Zoom, "follow-ups must target the replacement, not the old same-named window")
			}
			second, err := buildPlan(ctx, b, workspace, true)
			require.NoError(t, err)
			assert.True(t, second.IsEmpty(), "second force start must plan no actions, got %v", second.Actions)
		})
	}
}

func TestForceReplacementFailurePreservesOldWindows(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failCommand string
		windowCount int
	}{
		{name: "creation fails", failCommand: "new-window", windowCount: 2},
		{name: "follow-up fails", failCommand: "split-window", windowCount: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newForceTestBackend(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()

			directory, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			require.NoError(t, b.Apply(ctx, []backend.Action{
				backend.CreateSessionAction{Name: "dev", WindowName: "editor", Path: directory},
				backend.CreateWindowAction{Session: "dev", Name: "scratch", Path: directory},
				backend.CreateSessionAction{Name: "personal", WindowName: "shell", Path: directory},
			}))
			before, err := b.QueryState(ctx)
			require.NoError(t, err)
			devBefore := findBackendSession(t, before.Sessions, "dev")
			workspace := forceReplacementWorkspace(directory, []string{"editor"})
			service := NewService(func(...string) (backend.Backend, error) { return b, nil })
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }
			t.Setenv("HETKI_TEST_FAIL_COMMAND", tc.failCommand)

			err = service.Run(ctx, Options{Force: true})

			var exitError *exec.ExitError
			require.ErrorAs(t, err, &exitError)
			assert.Equal(t, 73, exitError.ExitCode())
			after, err := b.QueryState(ctx)
			require.NoError(t, err)
			assert.Equal(t, findBackendSession(t, before.Sessions, "personal"), findBackendSession(t, after.Sessions, "personal"))
			devAfter := findBackendSession(t, after.Sessions, "dev")
			assert.Equal(t, devBefore.ID, devAfter.ID)
			assert.Len(t, devAfter.Windows, tc.windowCount, "already-created resources are retained on failure")
			for _, window := range devBefore.Windows {
				retained := findBackendWindow(t, devAfter.Windows, window.Name)
				assert.Equal(t, window.ID, retained.ID)
				assert.Equal(t, window.Panes, retained.Panes)
			}
		})
	}
}

func forceReplacementWorkspace(directory string, names []string) *manifest.Workspace {
	windows := make([]manifest.Window, len(names))
	for i, name := range names {
		windows[i] = manifest.Window{
			Name:   name,
			Path:   directory,
			Layout: "tiled",
			Panes:  []manifest.Pane{{Path: directory}, {Path: directory, Zoom: true}},
		}
	}
	return &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: windows}}}
}

func newForceTestBackend(t *testing.T) *backendtmux.TmuxBackend {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is required for integration tests")
	}

	socketDir, err := os.MkdirTemp("/tmp", "hetki-force-")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(socketDir))
	})
	socket := filepath.Join(socketDir, "tmux.sock")
	config := filepath.Join(socketDir, "tmux.conf")
	configText := `set -g default-shell /bin/sh
set -g default-command "exec /bin/sh"
set -g base-index 1
set -g pane-base-index 1
set -g automatic-rename off
`
	require.NoError(t, os.WriteFile(config, []byte(configText), 0600))

	wrapperDir := t.TempDir()
	wrapper := fmt.Sprintf(`#!/bin/sh
case "$1" in attach-session|switch-client) exit 0;; esac
if [ "$1" = "$HETKI_TEST_FAIL_COMMAND" ]; then
  printf 'injected failure: %%s\n' "$1" >&2
  exit 73
fi
exec %q -S %q -f %q "$@"
`, realTmux, socket, config)
	require.NoError(t, os.WriteFile(filepath.Join(wrapperDir, "tmux"), []byte(wrapper), 0700))
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	t.Setenv("HETKI_TEST_FAIL_COMMAND", "")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		output, err := exec.CommandContext(ctx, realTmux, "-S", socket, "kill-server").CombinedOutput()
		if err != nil {
			t.Errorf("clean up isolated tmux server: %v: %s", err, output)
		}
	})

	b, err := backendtmux.NewBackend()
	require.NoError(t, err)
	return b
}
