//go:build integration

package tmux

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionStateDoesNotStartServer(t *testing.T) {
	b := newIsolatedTmuxBackend(t)
	t.Setenv("TMUX_PANE", "")

	snapshot, err := b.QueryActionState(t.Context())

	require.NoError(t, err)
	require.Nil(t, snapshot.Server)
	require.NotEmpty(t, snapshot.Socket)
	_, err = os.Stat(snapshot.Socket)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = b.client.Run(t.Context(), "new-session", "-d", "-s", "first", "/bin/sh")
	require.NoError(t, err)
	snapshot, err = b.QueryActionState(t.Context())
	require.NoError(t, err)
	require.NotNil(t, snapshot.Server)
	assert.Nil(t, snapshot.State.Invocation)
}

func TestActionStateNativeInvocation(t *testing.T) {
	for _, test := range []struct {
		name      string
		setup     func(*testing.T, navigationFixture)
		wantKnown bool
	}{
		{"pane", func(*testing.T, navigationFixture) {}, true},
		{"popup", func(t *testing.T, f navigationFixture) {
			t.Helper()
			f.attach(t, f.source.SessionID)
			t.Setenv("TMUX_PANE", "")
		}, true},
		{"popup with observer", func(t *testing.T, f navigationFixture) {
			t.Helper()
			f.attach(t, f.source.SessionID)
			f.observe(t)
			t.Setenv("TMUX_PANE", "")
		}, true},
		{"ambiguous popup", func(t *testing.T, f navigationFixture) {
			t.Helper()
			f.attach(t, f.source.SessionID)
			f.attach(t, f.source.SessionID)
			t.Setenv("TMUX_PANE", "")
		}, false},
		{"popup without client", func(t *testing.T, _ navigationFixture) {
			t.Helper()
			t.Setenv("TMUX_PANE", "")
		}, false},
		{"other server PID", func(t *testing.T, f navigationFixture) {
			t.Helper()
			pid, err := strconv.Atoi(f.run(t, "display-message", "-p", "#{pid}"))
			require.NoError(t, err)
			t.Setenv("TMUX", fmt.Sprintf("%s,%d,%s", f.run(t, "display-message", "-p", "#{socket_path}"), pid+1, strings.TrimPrefix(f.source.SessionID, "$")))
		}, false},
		{"other socket", func(t *testing.T, f navigationFixture) {
			t.Helper()
			t.Setenv("TMUX", "/other/socket,"+f.run(t, "display-message", "-p", "#{pid}")+",0")
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newNavigationFixture(t)
			test.setup(t, f)

			snapshot, err := f.backend.QueryActionState(f.ctx)

			require.NoError(t, err)
			require.NotNil(t, snapshot.State.Invocation)
			if test.wantKnown {
				assert.Equal(t, &backend.Invocation{SessionID: f.source.SessionID, PaneID: f.source.PaneID}, snapshot.State.Invocation)
			} else {
				assert.Equal(t, &backend.Invocation{}, snapshot.State.Invocation)
			}
		})
	}
}
