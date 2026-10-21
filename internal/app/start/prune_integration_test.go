//go:build integration

package start

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestPruneSessionsOnIsolatedTmux(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%t", force), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			b, service, workspace := nativePruningFixture(t, ctx)
			directory := workspace.Sessions[0].Windows[0].Path
			require.NoError(t, b.Apply(ctx, []backend.Action{backend.CreateWindowAction{Session: "dev", Name: "scratch", Path: directory}}))
			before, err := b.QueryState(ctx)
			require.NoError(t, err)
			dev := findBackendSession(t, before.Sessions, "dev")
			old := findBackendSession(t, before.Sessions, "old")
			opts := Options{Path: new("workspace.yaml"), Force: force, PruneSessions: true, DryRun: true}

			output := captureLoggerOutput(t, func() { require.NoError(t, service.Run(ctx, opts)) })
			afterDry, err := b.QueryState(ctx)
			require.NoError(t, err)
			require.Equal(t, before.Sessions, afterDry.Sessions)
			require.Contains(t, output, fmt.Sprintf("# Kill session: %q (%s)", old.Name, old.ID))
			require.Contains(t, output, "tmux kill-session -t '"+old.ID+"'")
			opts.DryRun = false
			require.NoError(t, service.Run(ctx, opts))

			after, err := b.QueryState(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"dev"}, backendSessionNames(after.Sessions))
			retained := findBackendSession(t, after.Sessions, "dev")
			require.Equal(t, dev.ID, retained.ID)
			require.Equal(t, dev.Windows[0].ID, findBackendWindow(t, retained.Windows, "editor").ID)
			wantWindows := []string{"editor", "tools", "scratch"}
			if force {
				wantWindows = []string{"editor", "tools"}
			}
			require.ElementsMatch(t, wantWindows, backendWindowNames(retained.Windows))
			second, err := buildPlan(ctx, b, workspace, opts)
			require.NoError(t, err)
			require.True(t, second.IsEmpty(), "repeated start must not recreate windows: %v", second.Actions)
		})
	}
}

func TestPruningStopsAfterReconciliationFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	b, service, _ := nativePruningFixture(t, ctx)
	before, err := b.QueryState(ctx)
	require.NoError(t, err)
	t.Setenv("HETKI_TEST_FAIL_COMMAND", "new-window")

	err = service.Run(ctx, Options{Path: new("workspace.yaml"), PruneSessions: true})

	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 73, exitError.ExitCode())
	after, err := b.QueryState(ctx)
	require.NoError(t, err)
	require.Equal(t, before.Sessions, after.Sessions)
}

func TestPruningFailureLeavesCompletedDeletionsAndStopsRemainingWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	b, service, workspace := nativePruningFixture(t, ctx)
	directory := workspace.Sessions[0].Windows[0].Path
	require.NoError(t, b.Apply(ctx, []backend.Action{
		backend.CreateSessionAction{Name: "old-b", WindowName: "shell", Path: directory},
		backend.CreateSessionAction{Name: "old-c", WindowName: "shell", Path: directory},
	}))
	before, err := b.QueryState(ctx)
	require.NoError(t, err)
	failed := findBackendSession(t, before.Sessions, "old-b")
	skipped := findBackendSession(t, before.Sessions, "old-c")
	recorder := &recordingPruneBackend{Backend: b}
	service.DetectBackend = func() (Backend, error) { return recorder, nil }
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) {
		return workspace, filepath.Join(directory, "workspace.yaml"), nil
	}
	t.Setenv("HETKI_TEST_FAIL_COMMAND", "kill-session")
	t.Setenv("HETKI_TEST_FAIL_TARGET", failed.ID)

	err = service.Run(ctx, Options{Path: new("workspace.yaml"), PruneSessions: true})

	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 73, exitError.ExitCode())
	require.ErrorContains(t, err, failed.ID)
	require.Contains(t, appshared.ErrorHint(err), "not rolled back")
	require.Equal(t, 1, recorder.applyCalls, "metadata must not be stamped after partial failure")
	require.Zero(t, recorder.attachCalls)
	after, err := b.QueryState(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"dev", "old-b", "old-c"}, backendSessionNames(after.Sessions))
	require.Equal(t, failed, findBackendSession(t, after.Sessions, "old-b"))
	require.Equal(t, skipped, findBackendSession(t, after.Sessions, "old-c"))
	dev := findBackendSession(t, after.Sessions, "dev")
	require.ElementsMatch(t, []string{"editor", "tools"}, backendWindowNames(dev.Windows), "completed reconciliation is not rolled back")
}

func TestPruningDryRunLeavesAnAttachedClientUnchanged(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	b, service, _ := nativePruningFixture(t, ctx)
	recorder := &recordingPruneBackend{Backend: b}
	service.DetectBackend = func() (Backend, error) { return recorder, nil }
	initial, err := b.QueryState(ctx)
	require.NoError(t, err)
	old := findBackendSession(t, initial.Sessions, "old")
	client := exec.CommandContext(ctx, "tmux", "-C", "attach-session", "-t", old.ID)
	client.WaitDelay = time.Second
	client.Stdout, client.Stderr = io.Discard, io.Discard
	input, err := client.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, client.Start())
	defer func() {
		closeErr := input.Close()
		waitErr := client.Wait()
		require.NoError(t, closeErr)
		require.NoError(t, waitErr)
	}()
	_, err = io.WriteString(input, "wait-for -S prune-client-ready\n")
	require.NoError(t, err)
	output, err := exec.CommandContext(ctx, "tmux", "wait-for", "prune-client-ready").CombinedOutput()
	require.NoError(t, err, "%s", output)
	clients := pruningClientNavigation(t, ctx)
	require.Equal(t, fmt.Sprintf("%d|%s|%s|%s", client.Process.Pid, old.ID, old.Windows[0].ID, old.Windows[0].Panes[0].ID), clients)

	for _, tc := range []struct {
		name   string
		inside bool
	}{
		{name: "outside preview"},
		{name: "invoking session refused", inside: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.inside {
				t.Setenv("TMUX", "/unused-test-socket,1,"+strings.TrimPrefix(old.ID, "$"))
				t.Setenv("TMUX_PANE", old.Windows[0].Panes[0].ID)
			}
			before, err := b.QueryState(ctx)
			require.NoError(t, err)

			err = service.Run(ctx, Options{Path: new("workspace.yaml"), PruneSessions: true, DryRun: true})

			if tc.inside {
				require.ErrorIs(t, err, ErrUnsafePruning)
			} else {
				require.NoError(t, err)
			}
			after, err := b.QueryState(ctx)
			require.NoError(t, err)
			require.Equal(t, before.Sessions, after.Sessions)
			require.Equal(t, clients, pruningClientNavigation(t, ctx))
			require.Zero(t, recorder.applyCalls)
			require.Zero(t, recorder.attachCalls)
		})
	}
}

func TestPruningPreservesLinkedWindowsUnlessForceWouldDestroyThem(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%t", force), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			b, service, _ := nativePruningFixture(t, ctx)
			initial, err := b.QueryState(ctx)
			require.NoError(t, err)
			dev := findBackendSession(t, initial.Sessions, "dev")
			old := findBackendSession(t, initial.Sessions, "old")
			output, err := exec.CommandContext(ctx, "tmux", "link-window", "-s", old.Windows[0].ID, "-t", dev.ID+":").CombinedOutput()
			require.NoError(t, err, "%s", output)
			before, err := b.QueryState(ctx)
			require.NoError(t, err)

			err = service.Run(ctx, Options{Path: new("workspace.yaml"), Force: force, PruneSessions: true})

			if force {
				require.ErrorIs(t, err, ErrUnsafePruning)
			} else {
				require.NoError(t, err)
			}
			after, err := b.QueryState(ctx)
			require.NoError(t, err)
			if force {
				require.Equal(t, before.Sessions, after.Sessions)
				return
			}
			require.Equal(t, []string{"dev"}, backendSessionNames(after.Sessions))
			retained := findBackendWindow(t, after.Sessions[0].Windows, "shell")
			require.Equal(t, old.Windows[0].ID, retained.ID)
			require.Equal(t, old.Windows[0].Panes, retained.Panes)
		})
	}
}

func TestPruningNeverFallsBackToARecreatedSessionName(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	b, _, _ := nativePruningFixture(t, ctx)
	before, err := b.QueryState(ctx)
	require.NoError(t, err)
	old := findBackendSession(t, before.Sessions, "old")
	target := backend.KillSessionAction{Name: old.ID}
	require.NoError(t, b.Apply(ctx, []backend.Action{target, backend.CreateSessionAction{Name: old.Name, WindowName: "replacement", Path: old.Windows[0].Path}}))
	replacement, err := b.QueryState(ctx)
	require.NoError(t, err)
	require.NotEqual(t, old.ID, findBackendSession(t, replacement.Sessions, "old").ID)

	err = b.Apply(ctx, []backend.Action{target})

	require.Error(t, err)
	after, err := b.QueryState(ctx)
	require.NoError(t, err)
	require.Equal(t, replacement.Sessions, after.Sessions)
}

func nativePruningFixture(t *testing.T, ctx context.Context) (*backendtmux.TmuxBackend, Service, *manifest.Workspace) {
	t.Helper()
	b := newIsolatedStartBackend(t)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, b.Apply(ctx, []backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor", Path: directory},
		backend.CreateSessionAction{Name: "old", WindowName: "shell", Path: directory},
	}))
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{
		{Name: "editor", Path: directory, Panes: []manifest.Pane{{Path: directory}}},
		{Name: "tools", Path: directory, Panes: []manifest.Pane{{Path: directory}}},
	}}}}
	service := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }
	return b, service, workspace
}

type recordingPruneBackend struct {
	Backend
	applyCalls  int
	attachCalls int
}

func (b *recordingPruneBackend) Apply(ctx context.Context, actions []backend.Action) error {
	b.applyCalls++
	return b.Backend.Apply(ctx, actions)
}

func (b *recordingPruneBackend) Attach(ctx context.Context, session string) error {
	b.attachCalls++
	return b.Backend.Attach(ctx, session)
}

func pruningClientNavigation(t *testing.T, ctx context.Context) string {
	t.Helper()
	output, err := exec.CommandContext(ctx, "tmux", "list-clients", "-F", "#{client_pid}|#{session_id}|#{window_id}|#{pane_id}").CombinedOutput()
	require.NoError(t, err, "%s", output)
	return strings.TrimSpace(string(output))
}
