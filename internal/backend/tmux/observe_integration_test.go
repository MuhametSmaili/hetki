//go:build integration

package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func nativeStatusFixture(t *testing.T) (*TmuxBackend, observe.Server, string, string) {
	t.Helper()

	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is required for integration tests")
	}
	return nativeStatusFixtureWithBinary(t, bin)
}

func nativeStatusFixtureWithBinary(t *testing.T, bin string) (*TmuxBackend, observe.Server, string, string) {
	t.Helper()

	socketDir, err := os.MkdirTemp("", "hetki-observe-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(socketDir)) })
	socket := filepath.Join(socketDir, "tmux.sock")
	wrapper := filepath.Join(t.TempDir(), "tmux")
	script := fmt.Sprintf("#!/bin/sh\nunset TMUX\nexec %q -f /dev/null -S %q \"$@\"\n", bin, socket)
	require.NoError(t, os.WriteFile(wrapper, []byte(script), 0755))
	b := &TmuxBackend{client: &client{bin: wrapper}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = b.client.Run(ctx, "kill-server")
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	out, err := b.client.Run(ctx, "new-session", "-d", "-s", "native-status", "-x", "100", "-y", "30", "-P", "-F", "#{session_id}|#{pane_id}", "cat")
	require.NoError(t, err)
	fields := strings.Split(strings.TrimSpace(out), "|")
	require.Len(t, fields, 2)
	server, err := b.StatusServer(ctx)
	require.NoError(t, err)
	return b, server, fields[0], fields[1]
}

// The two explicit binaries let this test exercise different protocol families
// in one process without changing PATH or requiring mise in the test suite.
func TestNativeStatusMixedServerVersions(t *testing.T) {
	legacy := os.Getenv("HETKI_TEST_TMUX_LEGACY")
	current := os.Getenv("HETKI_TEST_TMUX_CURRENT")
	if legacy == "" || current == "" {
		t.Skip("set HETKI_TEST_TMUX_LEGACY and HETKI_TEST_TMUX_CURRENT to test both wire families")
	}

	cases := []struct {
		name string
		bin  string
	}{
		{"legacy", legacy},
		{"current", current},
	}
	ready := []chan struct{}{make(chan struct{}), make(chan struct{})}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, server, session, pane := nativeStatusFixtureWithBinary(t, tc.bin)
			requireNativeVersionFamily(t, server.Version, tc.name)
			c, err := observe.Open(t.Context(), server, session)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, c.Close()) })
			close(ready[i])
			awaitTmuxChannel(t, ready[1-i])

			assertNativeStatusDelivery(t, b, c, pane)
		})
	}
}

func requireNativeVersionFamily(t *testing.T, version, family string) {
	t.Helper()
	versions := []string{"3.6", "3.6a", "3.6b", "3.7", "3.7a", "3.7b", "3.7c", "3.8"}
	if family == "legacy" {
		versions = []string{"3.2", "3.2a", "3.3", "3.3a", "3.4", "3.5", "3.5a"}
	}
	require.Contains(t, versions, version)
}

func assertNativeStatusDelivery(t *testing.T, b *TmuxBackend, c *observe.Connection, pane string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	received := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- c.ReadOutput(ctx, func(event observe.PaneOutput) error {
			select {
			case received <- event.Pane:
			default:
			}
			return nil
		}, nil)
	}()
	t.Cleanup(func() {
		cancel()
		awaitTmuxChannel(t, done)
	})

	_, err := b.client.Run(ctx, "send-keys", "-t", pane, "-l", "--", "mixed-layout\n")
	require.NoError(t, err)
	observed := awaitTmuxChannel(t, received)
	cancel()
	readErr := awaitTmuxChannel(t, done)

	require.Equal(t, pane, observed)
	require.ErrorIs(t, readErr, context.Canceled)
}

func TestNativeStatusOutputPreservesBytesPaneAndGeometry(t *testing.T) {
	b, server, session, pane := nativeStatusFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	before, err := b.client.Run(ctx, "display-message", "-p", "-t", pane, "#{pane_width}|#{pane_height}")
	require.NoError(t, err)
	c, err := observe.Open(ctx, server, session)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, c.Close())
	})
	wanted := []byte("\x1b]7501;state=working\x07日本語\\status")
	received := make(chan string, 1)
	done := make(chan error, 1)
	readCtx, stopRead := context.WithCancel(ctx)
	defer stopRead()
	go func() {
		defer close(done)
		var output []byte
		done <- c.ReadOutput(readCtx, func(event observe.PaneOutput) error {
			if event.Pane != pane {
				return fmt.Errorf("unexpected pane %s", event.Pane)
			}
			if len(output)+len(event.Data) > 64<<10 {
				return errors.New("fixture output exceeded limit")
			}
			output = append(output, event.Data...)
			if bytes.Contains(output, wanted) {
				select {
				case received <- event.Pane:
				default:
				}
			}
			return nil
		}, nil)
	}()
	t.Cleanup(func() {
		stopRead()
		awaitTmuxChannel(t, done)
	})

	_, sendErr := b.client.Run(ctx, "send-keys", "-t", pane, "-l", "--", string(wanted)+"\n")
	require.NoError(t, sendErr)
	observed := awaitTmuxChannel(t, received)
	stopRead()
	readErr := awaitTmuxChannel(t, done)
	after, sizeErr := b.client.Run(ctx, "display-message", "-p", "-t", pane, "#{pane_width}|#{pane_height}")

	require.Equal(t, pane, observed)
	require.ErrorIs(t, readErr, context.Canceled)
	require.NoError(t, sizeErr)
	require.Equal(t, before, after)
	require.NotEmpty(t, c.Generation())
}

func TestNativeStatusHandshakeRejectsWrongServerIncarnation(t *testing.T) {
	_, server, session, _ := nativeStatusFixture(t)
	server.Started++

	c, err := observe.Open(t.Context(), server, session)

	require.Nil(t, c)
	require.ErrorIs(t, err, observe.ErrServerChanged)
}

func TestNativeStatusMissingSessionFailsStartup(t *testing.T) {
	_, server, _, _ := nativeStatusFixture(t)

	c, err := observe.Open(t.Context(), server, "$999999999")

	require.Nil(t, c)
	require.Error(t, err)
}

func TestNativeStatusShutdownAndReconnectHaveSeparateLifetimes(t *testing.T) {
	b, server, session, _ := nativeStatusFixture(t)
	first, err := observe.Open(t.Context(), server, session)
	require.NoError(t, err)
	require.NoError(t, first.Close())
	second, err := observe.Open(t.Context(), server, session)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, second.Close())
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- second.ReadOutput(ctx, func(observe.PaneOutput) error { return nil }, nil)
	}()
	t.Cleanup(func() {
		cancel()
		awaitTmuxChannel(t, done)
	})

	_, stopErr := b.client.Run(ctx, "kill-server")
	readErr := awaitTmuxChannel(t, done)

	require.NoError(t, stopErr)
	require.ErrorIs(t, readErr, observe.ErrStreamLost)
	require.NotEqual(t, first.Generation(), second.Generation())
	require.Eventually(t, func() bool {
		return server.Validate(ctx) != nil
	}, time.Second, time.Millisecond)
}

func TestNativeStatusOutputLossEndsObservation(t *testing.T) {
	b, server, session, _ := nativeStatusFixture(t)
	c, err := observe.Open(t.Context(), server, session)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, c.Close())
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- c.ReadOutput(ctx, func(observe.PaneOutput) error { return nil }, nil)
	}()
	t.Cleanup(func() {
		cancel()
		awaitTmuxChannel(t, done)
	})
	client := fmt.Sprintf("hetki-status-%d-%s", os.Getpid(), session)

	_, updateErr := b.client.Run(ctx, "refresh-client", "-t", client, "-f", "no-output")
	require.NoError(t, updateErr)
	readErr := awaitTmuxChannel(t, done)

	require.ErrorIs(t, readErr, observe.ErrStreamLost)
	require.NotErrorIs(t, readErr, context.DeadlineExceeded)
}
