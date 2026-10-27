//go:build integration

package statusobserver

import (
	"context"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func TestNativeProtocolReportsReachCacheAndSurviveAttachedExit(t *testing.T) {
	runner, server, sampler, id, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, err := runner.RunLimited(ctx, 4096, "set-window-option", "-t", id, "remain-on-exit", "on")
	require.NoError(t, err)
	source, err := NewSource(ctx, server, sampler, nil, func(err error) {
		t.Errorf("unexpected source error: %v", err)
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Close()) }()
	ref := awaitSourcePane(t, source, id, "")

	for _, tc := range []struct {
		payload string
		want    status.State
	}{
		{"state=working", status.Working},
		{"state=blocked", status.Blocked},
		{"state=idle", status.Idle},
		{"state=clear", status.Withdrawn},
		{"state=done", status.Unknown},
	} {
		_, err = runner.RunLimited(ctx, 4096, "send-keys", "-t", id, "-l", "--", string(protocolReport(tc.payload))+"\n")
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			snapshot, err := source.ReadStatus(ctx)
			return err == nil && snapshot.At(ref, time.Now()) == tc.want
		}, 3*time.Second, 10*time.Millisecond, tc.payload)
	}

	_, err = runner.RunLimited(ctx, 4096, "send-keys", "-t", id, "C-d")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		source.mu.Lock()
		defer source.mu.Unlock()
		return source.panes[id].facts.Dead
	}, 3*time.Second, 10*time.Millisecond)
	source.mu.Lock()
	row := source.panes[id]
	observation := row.protocol.pane.Snapshot()
	source.mu.Unlock()

	require.Equal(t, ref, row.pane.Ref)
	require.Equal(t, status.Completed, observation.Outcome)
	requireSourceState(t, source, ref, status.Unknown)
	_, err = runner.RunLimited(ctx, 4096, "respawn-pane", "-k", "-t", id, "stty -echo; exec cat")
	require.NoError(t, err)
	source.streams.signalChange()
	replacement := awaitSourcePane(t, source, id, ref.Generation)
	requireSourceState(t, source, replacement, status.Unreported)
}

func TestNativeReportAfterUnterminatedControlString(t *testing.T) {
	runner, server, sampler, id, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	source, err := NewSource(ctx, server, sampler, nil, func(err error) {
		t.Errorf("unexpected source error: %v", err)
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Close()) }()
	ref := awaitSourcePane(t, source, id, "")
	output := "\x1b_unterminated application string" + string(protocolReport("state=working"))

	_, err = runner.RunLimited(ctx, 4096, "send-keys", "-t", id, "-l", "--", output+"\n")

	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot, err := source.ReadStatus(ctx)
		return err == nil && snapshot.At(ref, time.Now()) == status.Working
	}, 3*time.Second, 10*time.Millisecond, "the report after the unterminated string must apply")
}

func sendShellInput(t *testing.T, ctx context.Context, runner nativeRunner, pane, input string) {
	t.Helper()
	_, err := runner.RunLimited(ctx, 4096, "send-keys", "-t", pane, "-l", "--", input+"\n")
	require.NoError(t, err)
}
