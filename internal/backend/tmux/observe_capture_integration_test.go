//go:build integration

package tmux

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
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
