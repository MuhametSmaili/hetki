package tmux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapturePaneUsesOneBoundedVisibleScreenRead(t *testing.T) {
	for _, test := range []struct {
		name   string
		target backend.Target
		native string
	}{
		{"session active pane", backend.Target{SessionID: "$0"}, "$0:"},
		{"window active pane", backend.Target{SessionID: "$12", WindowID: "@34"}, "$12:@34"},
		{"pane occurrence", backend.Target{SessionID: "$12", WindowID: "@34", PaneID: "%123"}, "$12:@34.%123"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var b backend.Backend = &TmuxBackend{client: &MockClient{
				RunFunc: func(context.Context, ...string) (string, error) {
					t.Fatal("capture must not query state or use unbounded Run")
					return "", nil
				},
				ExecuteFunc: func(context.Context, Action) error {
					t.Fatal("capture must not mutate tmux")
					return nil
				},
				RunLimitedFunc: func(ctx context.Context, limit int, args ...string) (string, error) {
					calls++
					assert.Equal(t, 1<<20, limit)
					assert.Equal(t, []string{"capture-pane", "-p", "-e", "-N", "-S", "0", "-t", test.native}, args)
					deadline, ok := ctx.Deadline()
					assert.True(t, ok)
					assert.InDelta(t, 1, time.Until(deadline).Seconds(), 0.1)
					return "  \x1b[31mred\x1b[0m  \n\n", nil
				},
			}}

			output, err := b.CapturePane(context.Background(), test.target)

			require.NoError(t, err)
			assert.Equal(t, "  \x1b[31mred\x1b[0m  \n\n", output)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestCapturePaneDiscardsFailedRead(t *testing.T) {
	failure := errors.New("stale pane")
	b := &TmuxBackend{client: &MockClient{RunLimitedFunc: func(context.Context, int, ...string) (string, error) {
		return "partial snapshot", failure
	}}}
	output, err := b.CapturePane(context.Background(), backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"})
	require.ErrorIs(t, err, failure)
	assert.Empty(t, output)
}

func TestCapturePaneProcessCancellationAndTimeout(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tmux")
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("CAPTURE_STARTED", started)
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\necho started > \"$CAPTURE_STARTED\"\nexec sleep 10\n"), 0755))
	b := &TmuxBackend{client: &client{bin: bin}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.CapturePane(ctx, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"})
	require.ErrorIs(t, err, context.Canceled)
	_, err = os.Stat(started)
	require.True(t, os.IsNotExist(err), "already-canceled capture must not dispatch")

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := b.CapturePane(ctx, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"})
		done <- err
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, awaitTmuxChannel(t, done), context.Canceled)

	start := time.Now()
	_, err = b.CapturePane(context.Background(), backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second, "capture owns a one-second deadline")

}
