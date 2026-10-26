//go:build integration

package tmux

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func TestNativeStatusInventoryAndCaptureRegions(t *testing.T) {
	b, server, session, pane := nativeStatusFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sampler, err := b.StatusSampler(server)
	require.NoError(t, err)
	_, err = b.client.Run(ctx, "respawn-pane", "-k", "-t", pane,
		`printf '\033[2J\033[1;1HTOP\033[30;1HBOTTOM'; exec cat`)
	require.NoError(t, err)
	waitForNativeScreen(t, ctx, b, pane, "BOTTOM")
	_, err = b.client.Run(ctx, "new-session", "-d", "-s", "linked", "sleep 60", ";", "link-window", "-s", session+":0", "-t", "linked:")
	require.NoError(t, err)

	inventory, err := sampler.Inventory(ctx, 1)
	require.NoError(t, err)
	facts, found := inventory.Lookup(pane)
	links := 0
	for location := range inventory.Locations() {
		if location.Pane == pane {
			links++
		}
	}

	require.True(t, found)
	require.NoError(t, inventory.Problem(pane))
	require.NotEmpty(t, facts.Root.Identity())
	require.NotEmpty(t, facts.Foreground.Identity())
	require.Equal(t, 2, links)
	var targets []observe.CaptureTarget
	for id := range inventory.All() {
		target, targetErr := inventory.Target(id, observe.Region{Bottom: true, Rows: 1})
		require.NoError(t, targetErr)
		targets = append(targets, target)
	}

	mixed, err := sampler.Capture(ctx, targets)

	require.NoError(t, err)
	require.Len(t, mixed, 2)
	for _, screen := range mixed {
		require.NoError(t, screen.Err)
		require.Equal(t, 1, strings.Count(screen.Text, "\n"))
	}
	cases := []struct {
		name   string
		region observe.Region
		want   string
		absent string
		rows   int
	}{
		{"whole viewport", observe.Region{}, "BOTTOM", "", 30},
		{"top", observe.Region{Rows: 1}, "TOP", "BOTTOM", 1},
		{"bottom", observe.Region{Bottom: true, Rows: 2}, "BOTTOM", "TOP", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, err := inventory.Target(pane, tc.region)
			require.NoError(t, err)

			screens, err := sampler.Capture(ctx, []observe.CaptureTarget{target})

			require.NoError(t, err)
			require.Len(t, screens, 1)
			require.NoError(t, screens[0].Err)
			require.Contains(t, screens[0].Text, tc.want)
			require.Equal(t, tc.rows, strings.Count(screens[0].Text, "\n"))
			if tc.absent != "" {
				require.NotContains(t, screens[0].Text, tc.absent)
			}
		})
	}
}

func TestNativeStatusCapturePreservesPeerWhenPaneShrinks(t *testing.T) {
	cases := []struct {
		name   string
		region observe.Region
	}{
		{"whole viewport", observe.Region{}},
		{"bottom beyond resized viewport", observe.Region{Bottom: true, Rows: 2}},
		{"partly clipped range", observe.Region{Start: 18, Rows: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, server, _, pane := nativeStatusFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			peer, err := b.client.Run(ctx, "new-window", "-d", "-t", "native-status", "-P", "-F", "#{pane_id}",
				"printf 'PEER-CAPTURE\\n'; exec cat")
			require.NoError(t, err)
			peer = strings.TrimSpace(peer)
			waitForNativeScreen(t, ctx, b, peer, "PEER-CAPTURE")
			waitForNativeCommand(t, ctx, b, peer, "cat")
			sampler, err := b.StatusSampler(server)
			require.NoError(t, err)
			inventory, err := sampler.Inventory(ctx, 1)
			require.NoError(t, err)
			first, err := inventory.Target(pane, tc.region)
			require.NoError(t, err)
			second, err := inventory.Target(peer, observe.Region{})
			require.NoError(t, err)
			_, err = b.client.Run(ctx, "resize-window", "-t", pane, "-y", "20")
			require.NoError(t, err)

			screens, err := sampler.Capture(ctx, []observe.CaptureTarget{first, second})

			require.NoError(t, err)
			require.Len(t, screens, 2)
			require.ErrorIs(t, screens[0].Err, observe.ErrChanged)
			require.Empty(t, screens[0].Text)
			require.True(t, screens[0].StartedAt.IsZero())
			require.NoError(t, screens[1].Err)
			require.Contains(t, screens[1].Text, "PEER-CAPTURE")
			require.False(t, screens[1].StartedAt.IsZero())
		})
	}
}

func TestNativeStatusCaptureKeepsPeerWhenPaneCloses(t *testing.T) {
	b, server, _, pane := nativeStatusFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	peer, err := b.client.Run(ctx, "new-window", "-d", "-t", "native-status", "-P", "-F", "#{pane_id}",
		"printf 'PEER-CAPTURE\\n'; exec cat")
	require.NoError(t, err)
	peer = strings.TrimSpace(peer)
	waitForNativeScreen(t, ctx, b, peer, "PEER-CAPTURE")
	waitForNativeCommand(t, ctx, b, peer, "cat")
	sampler, err := b.StatusSampler(server)
	require.NoError(t, err)
	inventory, err := sampler.Inventory(ctx, 1)
	require.NoError(t, err)
	closing, err := inventory.Target(pane, observe.Region{})
	require.NoError(t, err)
	surviving, err := inventory.Target(peer, observe.Region{})
	require.NoError(t, err)
	_, err = b.client.Run(ctx, "kill-pane", "-t", pane)
	require.NoError(t, err)

	screens, err := sampler.Capture(ctx, []observe.CaptureTarget{closing, surviving})

	require.NoError(t, err)
	require.Len(t, screens, 2)
	require.ErrorIs(t, screens[0].Err, observe.ErrChanged)
	require.Empty(t, screens[0].Text)
	require.NoError(t, screens[1].Err)
	require.Contains(t, screens[1].Text, "PEER-CAPTURE")
}

func TestNativeStatusCaptureKeepsPeerOfOversizedScreen(t *testing.T) {
	b, server, _, pane := nativeStatusFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	waitForNativeCommand(t, ctx, b, pane, "cat")
	wide, err := b.client.Run(ctx, "new-session", "-d", "-s", "wide", "-x", "1000", "-y", "200", "-P", "-F", "#{pane_id}",
		`python3 -c "import sys,time; sys.stdout.write(('─'*1000)*199 + '─'*997 + 'END'); sys.stdout.flush(); time.sleep(30)"`)
	require.NoError(t, err)
	wide = strings.TrimSpace(wide)
	waitForNativeScreen(t, ctx, b, wide, "END")
	sampler, err := b.StatusSampler(server)
	require.NoError(t, err)
	inventory, err := sampler.Inventory(ctx, 1)
	require.NoError(t, err)
	small, err := inventory.Target(pane, observe.Region{})
	require.NoError(t, err)
	large, err := inventory.Target(wide, observe.Region{})
	require.NoError(t, err)

	screens, err := sampler.Capture(ctx, []observe.CaptureTarget{small, large})

	require.NoError(t, err)
	require.Len(t, screens, 2)
	require.NoError(t, screens[0].Err)
	require.False(t, screens[0].StartedAt.IsZero())
	require.ErrorIs(t, screens[1].Err, observe.ErrScreenTooLarge)
	require.Empty(t, screens[1].Text)
}

func TestNativeStatusCaptureRejectsRespawnAndRetainsDeadPane(t *testing.T) {
	b, server, _, pane := nativeStatusFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sampler, err := b.StatusSampler(server)
	require.NoError(t, err)
	inventory, err := sampler.Inventory(ctx, 1)
	require.NoError(t, err)
	target, err := inventory.Target(pane, observe.Region{})
	require.NoError(t, err)
	_, err = b.client.Run(ctx, "set-window-option", "-t", pane, "remain-on-exit", "on", ";",
		"respawn-pane", "-k", "-t", pane, "sleep 60")
	require.NoError(t, err)

	screens, err := sampler.Capture(ctx, []observe.CaptureTarget{target})

	require.NoError(t, err)
	require.Len(t, screens, 1)
	require.ErrorIs(t, screens[0].Err, observe.ErrChanged)
	require.Empty(t, screens[0].Text)
	_, err = b.client.Run(ctx, "respawn-pane", "-k", "-t", pane, "exit 0")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		out, queryErr := b.client.Run(ctx, "display-message", "-p", "-t", pane, "#{pane_dead}")
		return queryErr == nil && strings.TrimSpace(out) == "1"
	}, 2*time.Second, 10*time.Millisecond)

	dead, err := sampler.Inventory(ctx, 2)
	deadPane, found := dead.Lookup(pane)

	require.NoError(t, err)
	require.True(t, found)
	require.True(t, deadPane.Dead)
	require.NoError(t, dead.Problem(pane))
	_, err = dead.Target(pane, observe.Region{})
	require.Error(t, err)
}

func TestNativeStatusInventoryForegroundHandoff(t *testing.T) {
	b, server, _, pane := nativeStatusFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	sampler, err := b.StatusSampler(server)
	require.NoError(t, err)
	_, err = b.client.Run(ctx, "respawn-pane", "-k", "-t", pane, "env ENV= PS1='hetki-ready> ' /bin/sh -i")
	require.NoError(t, err)
	waitForNativeScreen(t, ctx, b, pane, "hetki-ready>")
	before, err := sampler.Inventory(ctx, 1)
	require.NoError(t, err)
	root, found := before.Lookup(pane)
	require.True(t, found)
	require.NoError(t, before.Problem(pane))
	target, err := before.Target(pane, observe.Region{})
	require.NoError(t, err)
	_, err = b.client.Run(ctx, "send-keys", "-t", pane, "-l", "--", "sleep 60\n")
	require.NoError(t, err)
	waitForNativeCommand(t, ctx, b, pane, "sleep")

	after, err := sampler.Inventory(ctx, 2)
	child, found := after.Lookup(pane)
	require.NoError(t, err)
	require.True(t, found)
	stale, err := sampler.Capture(ctx, []observe.CaptureTarget{target})

	require.NoError(t, err)
	require.NoError(t, after.Problem(pane))
	require.Equal(t, root.Root, child.Root)
	require.NotEqual(t, root.Foreground, child.Foreground)
	require.Equal(t, root.Root, root.Foreground)
	require.ErrorIs(t, stale[0].Err, observe.ErrChanged)
	require.Empty(t, stale[0].Text)
}

func waitForNativeCommand(t *testing.T, ctx context.Context, b *TmuxBackend, pane, command string) {
	t.Helper()
	require.Eventually(t, func() bool {
		out, err := b.client.Run(ctx, "display-message", "-p", "-t", pane, "#{pane_current_command}")
		return err == nil && strings.TrimSpace(out) == command
	}, 2*time.Second, 10*time.Millisecond)
}

func waitForNativeScreen(t *testing.T, ctx context.Context, b *TmuxBackend, pane, text string) {
	t.Helper()
	require.Eventually(t, func() bool {
		out, err := b.client.Run(ctx, "capture-pane", "-p", "-t", pane)
		return err == nil && strings.Contains(out, text)
	}, 2*time.Second, 10*time.Millisecond)
}
