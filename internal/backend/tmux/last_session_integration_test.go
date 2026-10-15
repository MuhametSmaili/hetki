//go:build integration

package tmux

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func TestLastSessionUsesTheInvokingClientOnAnIsolatedServer(t *testing.T) {
	b := newIsolatedTmuxBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		output, err := b.client.Run(ctx, args...)
		require.NoError(t, err)
		return strings.TrimSpace(output)
	}
	for _, session := range []string{"current", "previous", "other"} {
		run("new-session", "-d", "-s", session, "sleep 60")
	}
	previousID := run("display-message", "-p", "-t", "previous", "#{session_id}")
	attach := func(session string, count int) {
		t.Helper()
		cmd := exec.CommandContext(ctx, b.client.(*client).bin, "-C", "attach-session", "-t", session)
		input, err := cmd.StdinPipe()
		require.NoError(t, err)
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			_ = input.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		require.Eventually(t, func() bool {
			return len(strings.Fields(run("list-clients", "-F", "#{client_pid}"))) == count
		}, 3*time.Second, 10*time.Millisecond)
	}
	attach("current", 1)
	owner := run("list-clients", "-F", "#{client_name}")
	run("switch-client", "-c", owner, "-t", "previous")
	run("switch-client", "-c", owner, "-t", "current")
	attach("other", 2) // More recent does not mean it is our client.
	t.Setenv("TMUX", ",,0")
	t.Setenv("TMUX_PANE", "") // A popup inherits the session, not a pane ID.

	requireLastSessionMarker(t, ctx, b, "before observer", previousID)
	server, err := b.StatusServer(ctx)
	require.NoError(t, err)
	currentID := run("display-message", "-p", "-t", "current", "#{session_id}")
	observer, err := observe.Open(ctx, server, currentID)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, observer.Close())
	})

	requireLastSessionMarker(t, ctx, b, "observer attached popup", previousID)
	t.Setenv("TMUX_PANE", run("display-message", "-p", "-t", "current", "#{pane_id}"))
	requireLastSessionMarker(t, ctx, b, "observer attached shell", previousID)
	run("rename-session", "-t", previousID, "renamed")
	requireLastSessionMarker(t, ctx, b, "renamed previous session", previousID)

	other := run("list-clients", "-t", "other", "-F", "#{client_name}")
	run("switch-client", "-c", other, "-t", "current")
	requireLastSessionMarker(t, ctx, b, "ambiguous real clients", "")
	run("switch-client", "-c", other, "-t", "other")
	requireLastSessionMarker(t, ctx, b, "owner unambiguous again", previousID)
	require.NoError(t, observer.Close())
	requireLastSessionMarker(t, ctx, b, "observer closed", previousID)

	run("kill-session", "-t", previousID)
	requireLastSessionMarker(t, ctx, b, "deleted previous session", "")
}

func requireLastSessionMarker(t *testing.T, ctx context.Context, b *TmuxBackend, stage, want string) {
	t.Helper()
	t.Run(stage, func(t *testing.T) {
		queries := []struct {
			name string
			read func(context.Context) (backend.StateResult, error)
		}{
			{"ordinary", b.QueryState},
			{"status-aware", func(ctx context.Context) (backend.StateResult, error) {
				state, _, err := b.QueryStatusState(ctx)
				return state, err
			}},
		}
		for _, query := range queries {
			t.Run(query.name, func(t *testing.T) {
				state, err := query.read(ctx)

				require.NoError(t, err)
				marked := ""
				for _, session := range state.Sessions {
					if session.Last {
						require.Empty(t, marked, "only one session may be marked")
						marked = session.ID
					}
				}
				require.Equal(t, want, marked)
			})
		}
	})
}
