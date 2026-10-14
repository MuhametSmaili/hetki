//go:build integration

package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNavigableTmuxBackend runs on a throwaway socket and logs attach/switch
// navigation instead of touching a real terminal.
func newNavigableTmuxBackend(t *testing.T) (*TmuxBackend, string) {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is required for integration tests")
	}
	socketDir, err := os.MkdirTemp("/tmp", "hetki-tmux-switch-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "tmux.sock")
	t.Cleanup(func() { _ = exec.Command(realTmux, "-S", socket, "kill-server").Run() })

	navLog := filepath.Join(socketDir, "nav.log")
	wrapperDir := t.TempDir()
	wrapper := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in attach-session|switch-client) printf '%%s\\n' \"$*\" >> %q; exit 0;; esac\nexec %q -f /dev/null -S %q \"$@\"\n", navLog, realTmux, socket)
	require.NoError(t, os.WriteFile(filepath.Join(wrapperDir, "tmux"), []byte(wrapper), 0755))
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	b, err := NewBackend()
	require.NoError(t, err)
	return b, navLog
}

func TestSwitchTargetsStableIDsOnColonNamedSessions(t *testing.T) {
	b, navLog := newNavigableTmuxBackend(t)

	require.NoError(t, b.client.Execute(context.Background(), CreateSession{Name: "a:b", WindowName: "w"}))
	state, err := RunQuery(context.Background(), b.client, LoadStateQuery{})
	require.NoError(t, err)
	require.Len(t, state.Sessions, 1)
	sessionID := state.Sessions[0].ID
	windowID := state.Sessions[0].Windows[0].ID
	windowIndex := state.Sessions[0].Windows[0].Index
	paneID := state.Sessions[0].Windows[0].Panes[0].ID
	paneIndex := state.Sessions[0].Windows[0].Panes[0].Index
	target := backend.Target{SessionID: sessionID, WindowID: windowID, PaneID: paneID}

	require.NoError(t, b.Switch(context.Background(), backend.Target{SessionID: sessionID}))
	require.NoError(t, b.Switch(context.Background(), backend.Target{SessionID: sessionID, WindowID: windowID}))
	require.NoError(t, b.Switch(context.Background(), target))

	stalePane, err := b.client.Run(context.Background(), "split-window", "-d", "-t", windowID, "-P", "-F", "#{pane_id}")
	require.NoError(t, err)
	stalePane = strings.TrimSpace(stalePane)
	_, err = b.client.Run(context.Background(), "kill-pane", "-t", stalePane)
	require.NoError(t, err)
	assert.Error(t, b.Switch(context.Background(), backend.Target{SessionID: sessionID, WindowID: windowID, PaneID: stalePane}), "stale pane ID must fail closed")

	t.Setenv("TMUX", fmt.Sprintf("%s,1,%s", "isolated", strings.TrimPrefix(sessionID, "$")))
	require.NoError(t, b.Switch(context.Background(), target))

	assert.Error(t, b.Switch(context.Background(), backend.Target{SessionID: sessionID, WindowID: "@999"}), "unknown window ID must fail closed")

	logged, err := os.ReadFile(navLog)
	require.NoError(t, err)
	assert.Equal(t, []string{
		fmt.Sprintf("attach-session -t %s", sessionID),
		fmt.Sprintf("attach-session -t %s:%d", sessionID, windowIndex),
		fmt.Sprintf("attach-session -t %s:%d.%d", sessionID, windowIndex, paneIndex),
		fmt.Sprintf("switch-client -t %s:%d.%d", sessionID, windowIndex, paneIndex),
	}, strings.Split(strings.TrimSpace(string(logged)), "\n"))
}

func TestSwitchScopedPaneKeepsTheSelectedLinkedSession(t *testing.T) {
	b, navLog := newNavigableTmuxBackend(t)
	ctx := context.Background()
	_, err := b.client.Run(ctx, "new-session", "-d", "-s", "first", "-P", "-F", "#{session_id}")
	require.NoError(t, err)
	state, err := RunQuery(ctx, b.client, LoadStateQuery{})
	require.NoError(t, err)
	window := state.Sessions[0].Windows[0]
	otherID, err := b.client.Run(ctx, "new-session", "-d", "-s", "second", "-P", "-F", "#{session_id}")
	require.NoError(t, err)
	otherID = strings.TrimSpace(otherID)
	_, err = b.client.Run(ctx, "link-window", "-s", window.ID, "-t", otherID+":9")
	require.NoError(t, err)
	paneID := window.Panes[0].ID
	target := backend.Target{SessionID: otherID, WindowID: window.ID, PaneID: paneID}
	require.NoError(t, b.Switch(ctx, target))
	_, err = b.client.Run(ctx, "unlink-window", "-t", otherID+":9")
	require.NoError(t, err)
	require.Error(t, b.Switch(ctx, target), "removed occurrence must not follow the surviving link")
	logged, err := os.ReadFile(navLog)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("attach-session -t %s:9.%d\n", otherID, window.Panes[0].Index), string(logged))
}
