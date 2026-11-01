//go:build integration

package tmux

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func captureFixture(t *testing.T) (*TmuxBackend, func(...string) string) {
	t.Helper()
	b := newIsolatedTmuxBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	return b, func(args ...string) string {
		t.Helper()
		output, err := b.client.Run(ctx, args...)
		require.NoError(t, err)
		return strings.TrimSuffix(output, "\n")
	}
}

func newCaptureSession(t *testing.T, ctx context.Context, b *TmuxBackend, name string, width, height int, command ...string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := b.client.Run(ctx, "new-session", "-d", "-s", name, "-P", "-F",
		"#{session_id}|#{window_id}|#{pane_id}", "/bin/sh", "-c", "exec sleep 60")
	require.NoError(t, err)
	ids := strings.Split(strings.TrimSpace(output), "|")
	require.Len(t, ids, 3)
	resizeCaptureWindow(t, ctx, b, ids[1], width, height)

	// Emit fixture output only after the pane has its asserted dimensions.
	args := append([]string{"respawn-pane", "-k", "-t", ids[2]}, command...)
	_, err = b.client.Run(ctx, args...)
	require.NoError(t, err)
	return ids
}

func resizeCaptureWindow(t *testing.T, ctx context.Context, b *TmuxBackend, target string, width, height int) {
	t.Helper()
	columns, rows := strconv.Itoa(width), strconv.Itoa(height)
	// Pin only an existing window; global manual sizing crashes tmux 3.4 on creation.
	_, err := b.client.Run(ctx, "resize-window", "-t", target, "-x", columns, "-y", rows)
	require.NoError(t, err)
	geometry, err := b.client.Run(ctx, "display-message", "-p", "-t", target, "#{pane_width}|#{pane_height}")
	require.NoError(t, err)
	require.Equal(t, columns+"|"+rows, strings.TrimSpace(geometry))
}
