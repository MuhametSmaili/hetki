//go:build integration

package statusobserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/screen"
	"github.com/MuhametSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestNativeSourceSelectsScreensAndTracksRespawn(t *testing.T) {
	runner, server, sampler, id, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	out, err := runner.RunLimited(ctx, 4096, "new-window", "-d", "-t", "first:", "-P", "-F", "#{pane_id}", "cat")
	require.NoError(t, err)
	peerID := strings.TrimSpace(out)
	source, err := NewSource(ctx, server, sampler, func(pane observe.Pane) Selection {
		if pane.ID != id {
			return Selection{}
		}
		return Selection{
			Profile: service.Profile{Source: service.Screen, Generation: "fixture-rules"},
			Evaluate: func(text string) (screen.Result, error) {
				if strings.Contains(text, "SOURCE-READY") {
					return screen.Result{State: status.Idle, Reason: screen.Matched}, nil
				}
				return screen.Result{State: status.Unknown, Reason: screen.NoMatch}, nil
			},
		}
	}, func(err error) { t.Errorf("unexpected source error: %v", err) })
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Close()) }()
	before := awaitSourcePane(t, source, id, "")
	peer := awaitSourcePane(t, source, peerID, "")
	_, err = runner.RunLimited(ctx, 4096, "send-keys", "-t", id, "-l", "--", "SOURCE-READY\n")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot, err := source.ReadStatus(ctx)
		return err == nil && snapshot.At(before, time.Now()) == status.Idle
	}, 3*time.Second, 10*time.Millisecond)

	_, err = runner.RunLimited(ctx, 4096, "respawn-pane", "-k", "-t", id, "stty -echo; exec cat")
	require.NoError(t, err)
	source.streams.signalChange()
	after := awaitSourcePane(t, source, id, before.Generation)
	snapshot, err := source.ReadStatus(ctx)

	require.NoError(t, err)
	require.NotEqual(t, before, after)
	require.Equal(t, status.Unknown, snapshot.At(after, time.Now()))
	require.Equal(t, status.Unreported, snapshot.At(peer, time.Now()))
	source.screens.mu.Lock()
	_, selectedPeer := source.screens.entries[peerID]
	source.screens.mu.Unlock()
	require.False(t, selectedPeer)
	cancel()
	await(t, source.done)
	source.streams.mu.Lock()
	closed := source.streams.closed
	source.streams.mu.Unlock()
	require.True(t, closed, "cancellation must join subscriptions before source completion")
	require.NoError(t, source.Close())
	_, err = source.ReadStatus(t.Context())
	require.ErrorIs(t, err, service.ErrClosed)
}

func awaitSourcePane(t *testing.T, source *Source, id, previous string) status.PaneRef {
	t.Helper()
	var ref status.PaneRef
	require.Eventually(t, func() bool {
		source.mu.Lock()
		defer source.mu.Unlock()
		row, found := source.panes[id]
		ref = row.pane.Ref
		return found && ref.Generation != previous && row.pane.Scope.Connection != ""
	}, 3*time.Second, 10*time.Millisecond)
	return ref
}

func TestNativeSourceInvalidSelectionStopsAndJoins(t *testing.T) {
	_, server, sampler, _, _ := nativeStreamsFixture(t)
	reported := make(chan error, 1)
	source, err := NewSource(t.Context(), server, sampler, func(observe.Pane) Selection {
		return Selection{Profile: service.Profile{Source: service.Screen, Generation: "missing-evaluator"}}
	}, func(err error) { reported <- err })
	require.NoError(t, err)
	defer func() { require.Error(t, source.Close()) }()

	await(t, source.done)
	_, readErr := source.ReadStatus(t.Context())
	closeErr := source.Close()

	require.ErrorContains(t, readErr, "screen profile requires an evaluator")
	require.ErrorContains(t, closeErr, "screen profile requires an evaluator")
	select {
	case err := <-reported:
		require.ErrorContains(t, err, "screen profile requires an evaluator")
	default:
		t.Fatal("terminal source failure was not reported")
	}
}
