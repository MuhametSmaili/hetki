//go:build integration

package tmux

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
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
	for _, test := range []struct {
		name   string
		target backend.Target
		want   string
	}{
		{"inactive pane", backend.Target{SessionID: session, WindowID: window, PaneID: left}, "LEFT  "},
		{"active pane", backend.Target{SessionID: session, WindowID: window, PaneID: right}, "RIGHT  "},
		{"session active pane", backend.Target{SessionID: session}, "OTHER-WINDOW"},
		{"inactive window", backend.Target{SessionID: session, WindowID: window}, "RIGHT  "},
		{"active window", backend.Target{SessionID: session, WindowID: other[0]}, "OTHER-WINDOW"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := b.CapturePane(context.Background(), test.target)

			require.NoError(t, err)
			assert.Contains(t, output, test.want)
			if test.want == "RIGHT  " {
				assert.Contains(t, output, "\x1b[31m", "capture must preserve styling")
				assert.NotContains(t, output, "LEFT")
			}
		})
	}
	assert.Equal(t, before, state(), "capture must not change active panes/windows or copy-mode selection")
	assert.Equal(t, buffers, run("list-buffers", "-F", "#{buffer_name}|#{buffer_size}|#{buffer_sample}"))
	assert.Equal(t, "paste buffer must survive", run("show-buffer", "-b", "sentinel"))

	// Session targets follow the current active window, never a cached pane.
	run("select-window", "-t", window)
	output, err := b.CapturePane(context.Background(), backend.Target{SessionID: session})
	require.NoError(t, err)
	assert.Contains(t, output, "RIGHT  ")

	run("select-pane", "-t", left)
	output, err = b.CapturePane(context.Background(), backend.Target{SessionID: session, WindowID: window})
	require.NoError(t, err)
	assert.Contains(t, output, "LEFT  ")

	stale := strings.Split(run("new-session", "-d", "-s", "stale", "-P", "-F", "#{session_id}|#{window_id}|#{pane_id}", "sleep 60"), "|")
	require.Len(t, stale, 3)
	run("kill-session", "-t", stale[0])
	before = state()
	for _, test := range []struct {
		name   string
		target backend.Target
	}{
		{"missing session", backend.Target{SessionID: stale[0]}},
		{"missing occurrence", backend.Target{SessionID: stale[0], WindowID: stale[1], PaneID: stale[2]}},
		{"missing window", backend.Target{SessionID: session, WindowID: stale[1]}},
		{"window with missing session", backend.Target{SessionID: stale[0], WindowID: window}},
		{"window in another session", backend.Target{SessionID: session, WindowID: unrelatedWindow}},
		{"pane in another window", backend.Target{SessionID: session, WindowID: other[0], PaneID: left}},
		{"pane with wrong window", backend.Target{SessionID: session, WindowID: window, PaneID: other[1]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := b.CapturePane(context.Background(), test.target)

			require.Error(t, err)
			assert.Empty(t, output)
			t.Logf("native scoped capture rejected %+v: %v", test.target, err)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = b.CapturePane(ctx, backend.Target{SessionID: session, WindowID: window, PaneID: right})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, before, state(), "failed capture must not select a fallback or mutate state")
	assert.Equal(t, buffers, run("list-buffers", "-F", "#{buffer_name}|#{buffer_size}|#{buffer_sample}"))
}

func TestCapturePaneLinkedOccurrenceMustStillExistOnIsolatedServer(t *testing.T) {
	b, run := captureFixture(t)
	ids := strings.Split(run("new-session", "-d", "-s", "source", "-P", "-F", "#{session_id}|#{window_id}|#{pane_id}", "sh", "-c", "printf 'LINKED\\r\\n'; exec sleep 60"), "|")
	require.Len(t, ids, 3)
	otherSession := run("new-session", "-d", "-s", "linked", "-P", "-F", "#{session_id}", "sleep 60")
	require.Eventually(t, func() bool {
		return run("display-message", "-p", "-t", ids[2], "#{cursor_y}") == "1"
	}, 3*time.Second, 10*time.Millisecond)

	for _, test := range []struct{ name, pane string }{{"window", ""}, {"pane", ids[2]}} {
		target := backend.Target{SessionID: otherSession, WindowID: ids[1], PaneID: test.pane}
		run("link-window", "-s", ids[1], "-t", otherSession+":9")
		t.Run("linked "+test.name, func(t *testing.T) {
			output, err := b.CapturePane(context.Background(), target)

			require.NoError(t, err)
			assert.Contains(t, output, "LINKED")
		})

		run("unlink-window", "-t", otherSession+":9")
		t.Run("removed "+test.name, func(t *testing.T) {
			output, err := b.CapturePane(context.Background(), target)

			require.Error(t, err, "removed occurrence must not follow the surviving link")
			assert.Empty(t, output)
			t.Logf("native scoped capture rejected removed link %+v: %v", target, err)
		})
	}

	stalePane := run("split-window", "-d", "-t", ids[1], "-P", "-F", "#{pane_id}", "sleep 60")
	run("kill-pane", "-t", stalePane)
	output, err := b.CapturePane(context.Background(), backend.Target{SessionID: ids[0], WindowID: ids[1], PaneID: stalePane})
	require.Error(t, err, "a missing pane must not fall back to the active pane")
	assert.Empty(t, output)

	output, err = b.CapturePane(context.Background(), backend.Target{SessionID: ids[0], WindowID: ids[1], PaneID: ids[2]})
	require.NoError(t, err)
	assert.Contains(t, output, "LINKED", "the original occurrence survives")
}

func TestCapturePaneCurrentAlternateScreenOnIsolatedServer(t *testing.T) {
	b, run := captureFixture(t)
	ids := strings.Split(run("new-session", "-d", "-s", "alternate", "-x", "40", "-y", "24", "-P", "-F", "#{session_id}|#{window_id}|#{pane_id}", "sh", "-c", "printf 'NORMAL-ONLY\\r\\n\\033[?1049h\\033[H\\033[2JALT-ONLY\\r\\n0123456789012345678901234567890123456789WRAPPED\\r\\n'; exec sleep 60"), "|")
	require.Len(t, ids, 3)
	require.Eventually(t, func() bool {
		return run("display-message", "-p", "-t", ids[2], "#{alternate_on}|#{cursor_y}") == "1|3"
	}, 3*time.Second, 10*time.Millisecond)
	for _, target := range []backend.Target{
		{SessionID: ids[0], WindowID: ids[1], PaneID: ids[2]},
		{SessionID: ids[0]},
		{SessionID: ids[0], WindowID: ids[1]},
	} {
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
	ids := strings.Split(run("new-session", "-d", "-s", "tall", "-x", "40", "-y", "240", "-P", "-F", "#{session_id}|#{window_id}|#{pane_id}", "sh", "-c", "printf 'HISTORY-ONLY\\r\\n'; i=0; while [ \"$i\" -lt 260 ]; do printf 'row-%03d\\r\\n' \"$i\"; i=$((i+1)); done; printf 'BOTTOM-ONLY'; exec sleep 60"), "|")
	require.Len(t, ids, 3)
	pane := ids[2]
	require.Eventually(t, func() bool {
		return run("display-message", "-p", "-t", pane, "#{cursor_y}|#{cursor_x}") == "239|11"
	}, 3*time.Second, 10*time.Millisecond)
	output, err := b.CapturePane(context.Background(), backend.Target{SessionID: ids[0], WindowID: ids[1], PaneID: pane})
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	require.Len(t, lines, 240)
	assert.Equal(t, "row-021", strings.TrimRight(lines[0], " "), "start at visible coordinate zero")
	assert.Equal(t, "row-259", strings.TrimRight(lines[238], " "))
	assert.Equal(t, "BOTTOM-ONLY", strings.TrimRight(lines[239], " "), "include the last visible row")
	assert.NotContains(t, output, "HISTORY-ONLY")
}
