//go:build integration

package tmux

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureFixture(t *testing.T) (*TmuxBackend, func(...string) string) {
	t.Helper()
	b := newIsolatedTmuxBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return b, func(args ...string) string {
		t.Helper()
		output, err := b.client.Run(ctx, args...)
		require.NoError(t, err)
		return strings.TrimSuffix(output, "\n")
	}
}

func TestCapturePaneStableTargetsAndNoMutationOnIsolatedServer(t *testing.T) {
	b, run := captureFixture(t)
	ids := strings.Split(run("new-session", "-d", "-s", "capture", "-x", "80", "-y", "24", "-P", "-F", "#{session_id}|#{window_id}|#{pane_id}", "sh", "-c", "printf 'LEFT  \\r\\n'; exec sleep 60"), "|")
	require.Len(t, ids, 3)
	session, window, left := ids[0], ids[1], ids[2]
	right := run("split-window", "-d", "-h", "-t", window, "-P", "-F", "#{pane_id}", "sh", "-c", "printf '\\033[31mRIGHT  \\033[0m\\r\\n'; exec sleep 60")
	other := strings.Split(run("new-window", "-d", "-t", session+":", "-P", "-F", "#{window_id}|#{pane_id}", "sh", "-c", "printf 'OTHER-WINDOW\\r\\n'; exec sleep 60"), "|")
	require.Len(t, other, 2)
	unrelatedWindow := run("new-session", "-d", "-s", "unrelated", "-P", "-F", "#{window_id}", "sleep 60")
	run("select-pane", "-t", right)
	run("select-window", "-t", other[0])

	for _, pane := range []string{left, right, other[1]} {
		require.Eventually(t, func() bool {
			return run("display-message", "-p", "-t", pane, "#{cursor_y}") == "1"
		}, 3*time.Second, 10*time.Millisecond)
	}
	run("set-buffer", "-b", "sentinel", "paste buffer must survive")
	run("copy-mode", "-t", right)
	run("send-keys", "-X", "-t", right, "cursor-up")
	run("send-keys", "-X", "-t", right, "start-of-line")
	run("send-keys", "-X", "-t", right, "begin-selection")
	run("send-keys", "-X", "-t", right, "cursor-right")
	require.Equal(t, "1", run("display-message", "-p", "-t", right, "#{selection_present}"))

	state := func() string {
		return run("list-panes", "-a", "-F", "#{session_id}|#{window_id}|#{window_active}|#{pane_id}|#{pane_active}|#{window_zoomed_flag}|#{pane_in_mode}|#{selection_present}|#{selection_start_x}|#{selection_start_y}|#{selection_end_x}|#{selection_end_y}|#{scroll_position}")
	}
	before := state()
	buffers := run("list-buffers", "-F", "#{buffer_name}|#{buffer_size}|#{buffer_sample}")
	for _, test := range []struct{ target, want string }{
		{left, "LEFT  "},
		{right, "RIGHT  "},
		{session + ":", "OTHER-WINDOW"},
		{session + ":" + window, "RIGHT  "},
		{session + ":" + other[0], "OTHER-WINDOW"},
	} {
		output, err := b.CapturePane(context.Background(), test.target)
		require.NoError(t, err, test.target)
		assert.Contains(t, output, test.want, test.target)
		if test.want == "RIGHT  " {
			assert.Contains(t, output, "\x1b[31m", "capture must preserve styling")
			assert.NotContains(t, output, "LEFT")
		}
	}
	assert.Equal(t, before, state(), "capture must not change active panes/windows or copy-mode selection")
	assert.Equal(t, buffers, run("list-buffers", "-F", "#{buffer_name}|#{buffer_size}|#{buffer_sample}"))
	assert.Equal(t, "paste buffer must survive", run("show-buffer", "-b", "sentinel"))

	// Session targets follow the current active window, never a cached pane.
	run("select-window", "-t", window)
	output, err := b.CapturePane(context.Background(), session+":")
	require.NoError(t, err)
	assert.Contains(t, output, "RIGHT  ")

	stale := strings.Split(run("new-session", "-d", "-s", "stale", "-P", "-F", "#{session_id}|#{window_id}|#{pane_id}", "sleep 60"), "|")
	require.Len(t, stale, 3)
	run("kill-session", "-t", stale[0])
	before = state()
	for _, target := range []string{stale[2], stale[0] + ":", session + ":" + stale[1], stale[0] + ":" + window, session + ":" + unrelatedWindow, "$01:", "%01", session + ":" + window + ".0", "%0;kill-server"} {
		output, err := b.CapturePane(context.Background(), target)
		require.Error(t, err, target)
		assert.Empty(t, output)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = b.CapturePane(ctx, right)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, before, state(), "failed capture must not select a fallback or mutate state")
	assert.Equal(t, buffers, run("list-buffers", "-F", "#{buffer_name}|#{buffer_size}|#{buffer_sample}"))
}

func TestCapturePaneCurrentAlternateScreenOnIsolatedServer(t *testing.T) {
	b, run := captureFixture(t)
	ids := strings.Split(run("new-session", "-d", "-s", "alternate", "-x", "40", "-y", "24", "-P", "-F", "#{session_id}|#{window_id}|#{pane_id}", "sh", "-c", "printf 'NORMAL-ONLY\\r\\n\\033[?1049h\\033[H\\033[2JALT-ONLY\\r\\n0123456789012345678901234567890123456789WRAPPED\\r\\n'; exec sleep 60"), "|")
	require.Len(t, ids, 3)
	require.Eventually(t, func() bool {
		return run("display-message", "-p", "-t", ids[2], "#{alternate_on}|#{cursor_y}") == "1|3"
	}, 3*time.Second, 10*time.Millisecond)
	for _, target := range []string{ids[2], ids[0] + ":", ids[0] + ":" + ids[1]} {
		output, err := b.CapturePane(context.Background(), target)
		require.NoError(t, err)
		assert.Contains(t, output, "ALT-ONLY")
		assert.Contains(t, output, "0123456789012345678901234567890123456789\nWRAPPED", "do not join wrapped screen rows")
		assert.NotContains(t, output, "NORMAL-ONLY", "-a would capture the saved normal screen")
	}
	assert.Equal(t, "1|3", run("display-message", "-p", "-t", ids[2], "#{alternate_on}|#{cursor_y}"))
}

func TestCapturePaneVisibleCoordinatesOnIsolatedServer(t *testing.T) {
	b, run := captureFixture(t)
	pane := run("new-session", "-d", "-s", "tall", "-x", "40", "-y", "240", "-P", "-F", "#{pane_id}", "sh", "-c", "printf 'HISTORY-ONLY\\r\\n'; i=0; while [ \"$i\" -lt 260 ]; do printf 'row-%03d\\r\\n' \"$i\"; i=$((i+1)); done; printf 'BOTTOM-ONLY'; exec sleep 60")
	require.Eventually(t, func() bool {
		return run("display-message", "-p", "-t", pane, "#{cursor_y}|#{cursor_x}") == "239|11"
	}, 3*time.Second, 10*time.Millisecond)
	output, err := b.CapturePane(context.Background(), pane)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	require.Len(t, lines, 240)
	assert.Equal(t, "row-021", strings.TrimRight(lines[0], " "), "start at visible coordinate zero")
	assert.Equal(t, "row-259", strings.TrimRight(lines[238], " "))
	assert.Equal(t, "BOTTOM-ONLY", strings.TrimRight(lines[239], " "), "include the last visible row")
	assert.NotContains(t, output, "HISTORY-ONLY")
}
