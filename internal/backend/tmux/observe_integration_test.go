//go:build integration

package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func nativeStatusFixture(t *testing.T) (*TmuxBackend, observe.Server, string, string) {
	t.Helper()

	b := newIsolatedTmuxBackend(t)
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
		})
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
		done <- second.ReadOutput(ctx, func(observe.PaneOutput) error { return nil })
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
		done <- c.ReadOutput(ctx, func(observe.PaneOutput) error { return nil })
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
