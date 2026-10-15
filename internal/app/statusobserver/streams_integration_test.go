//go:build integration

package statusobserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
)

type nativeRunner struct {
	client  tmux.Client
	socket  string
	stopped *atomic.Bool
}

func TestNativeInventoryWatchDiscoversSessionsAndJoinsOnPublisherFailure(t *testing.T) {
	runner, server, sampler, _, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	streams, err := NewStreams(ctx, server, &recorder{})
	require.NoError(t, err)
	defer func() { require.NoError(t, streams.Close()) }()
	published := make(chan observe.Inventory, 4)
	done := make(chan error, 1)
	joined := make(chan struct{})
	failure := errors.New("fixture publisher stopped")
	var stop atomic.Bool
	go func() {
		defer close(joined)
		done <- Watch(ctx, sampler, streams, nil, func(inventory observe.Inventory, err error) error {
			if err != nil {
				return err
			}
			if stop.Load() {
				return failure
			}
			select {
			case published <- inventory:
			default:
			}
			return nil
		})
	}()
	defer func() {
		cancel()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Error("inventory watch did not join")
		}
	}()
	first := awaitInventory(t, ctx, published, "")

	out, err := runner.RunLimited(ctx, 4096, "new-session", "-d", "-s", "new-session", "-P", "-F", "#{pane_id}", "cat")
	require.NoError(t, err)
	second := awaitInventory(t, ctx, published, strings.TrimSpace(out))
	stop.Store(true)
	streams.signalChange()
	select {
	case err := <-done:
		require.ErrorIs(t, err, failure)
	case <-ctx.Done():
		t.Fatal("publisher error did not stop inventory watch")
	}

	require.Greater(t, second.Revision(), first.Revision())
	require.Equal(t, server.Identity(), second.Server())
}

func awaitInventory(t *testing.T, ctx context.Context, published <-chan observe.Inventory, pane string) observe.Inventory {
	t.Helper()
	for {
		select {
		case inventory := <-published:
			_, found := inventory.Lookup(pane)
			if pane == "" || found {
				return inventory
			}
		case <-ctx.Done():
			t.Fatal("inventory did not discover expected pane")
			return observe.Inventory{}
		}
	}
}

func (r nativeRunner) RunLimited(ctx context.Context, limit int, args ...string) (string, error) {
	return r.client.RunLimited(ctx, limit, append([]string{"-f", "/dev/null", "-S", r.socket}, args...)...)
}

func (r nativeRunner) stop(ctx context.Context) error {
	if r.stopped.Load() {
		return nil
	}

	_, err := r.RunLimited(ctx, 4096, "kill-server")
	if err == nil {
		r.stopped.Store(true)
	}
	return err
}

func nativeStreamsFixture(t *testing.T) (nativeRunner, observe.Server, *observe.Sampler, string, string) {
	t.Helper()
	client, err := tmux.New()
	if err != nil {
		t.Skip("tmux is required for native stream integration")
	}
	dir := testenv.PrivateHome(t)
	runner := nativeRunner{client: client, socket: filepath.Join(dir, "tmux.sock"), stopped: new(atomic.Bool)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runner.stop(ctx); err != nil {
			t.Errorf("stop private tmux server: %v", err)
		}
	})
	out, err := runner.RunLimited(t.Context(), 4096, "new-session", "-d", "-s", "first", "-P", "-F", "#{pane_id}|#{window_id}", "stty -echo; exec cat")
	require.NoError(t, err)
	ids := strings.Split(strings.TrimSpace(out), "|")
	require.Len(t, ids, 2)
	server, err := observe.ResolveServer(t.Context(), runner)
	require.NoError(t, err)
	sampler, err := observe.NewSampler(server, runner)
	require.NoError(t, err)
	return runner, server, sampler, ids[0], ids[1]
}

func TestNativeStreamsLinkedWindowHandoff(t *testing.T) {
	runner, server, sampler, pane, window := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := runner.RunLimited(ctx, 4096,
		"new-window", "-d", "-t", "first:", "cat", ";",
		"new-session", "-d", "-s", "second", "cat", ";",
		"link-window", "-s", window, "-t", "second:")
	require.NoError(t, err)
	record := &recorder{}
	streams, err := NewStreams(ctx, server, record)
	require.NoError(t, err)
	defer func() { require.NoError(t, streams.Close()) }()
	inventory, err := sampler.Inventory(ctx, 1)
	require.NoError(t, err)
	require.NoError(t, inventory.Problem(pane))
	require.NoError(t, streams.Reconcile(ctx, inventory))

	before := sendNativeMarker(t, ctx, runner, record, pane, "before-handoff")
	_, err = runner.RunLimited(ctx, 4096, "unlink-window", "-t", "first:0")
	require.NoError(t, err)
	next, err := sampler.Inventory(ctx, 2)
	require.NoError(t, err)
	require.NoError(t, streams.Reconcile(ctx, next))
	after := sendNativeMarker(t, ctx, runner, record, pane, "after-handoff")

	require.Equal(t, pane, before.Pane)
	require.Equal(t, pane, after.Pane)
	require.Equal(t, before.Root, after.Root)
	require.NotEqual(t, before.Connection, after.Connection)
	require.Greater(t, after.Generation, before.Generation)
}

func sendNativeMarker(t *testing.T, ctx context.Context, runner nativeRunner, record *recorder, pane, marker string) Scope {
	t.Helper()
	_, err := runner.RunLimited(ctx, 4096, "send-keys", "-t", pane, "-l", "--", marker+"\n")
	require.NoError(t, err)
	var result Scope
	require.Eventually(t, func() bool {
		outputs, _ := record.snapshot()
		var text strings.Builder
		for _, output := range outputs {
			if output.scope.Pane == pane {
				text.WriteString(output.text)
				result = output.scope
			}
		}
		return strings.Contains(text.String(), marker)
	}, 2*time.Second, 10*time.Millisecond)
	return result
}
