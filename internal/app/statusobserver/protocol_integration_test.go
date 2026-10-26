//go:build integration

package statusobserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/protocols"
	"github.com/stretchr/testify/assert"
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

func TestNativeShellBoundariesAcrossForegroundHandoff(t *testing.T) {
	runner, server, sampler, id, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	script := filepath.Join(t.TempDir(), "markers.sh")
	require.NoError(t, os.WriteFile(script, []byte(shellBoundaryFixture), 0o600))
	_, err := runner.RunLimited(ctx, 4096, "respawn-pane", "-k", "-t", id, fmt.Sprintf("exec /bin/sh %q", script))
	require.NoError(t, err)
	source, err := NewSource(ctx, server, sampler, nil, func(err error) {
		t.Errorf("unexpected source error: %v", err)
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Close()) }()
	ref := awaitSourcePane(t, source, id, "")

	sendShellInput(t, ctx, runner, id, "run")
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		source.mu.Lock()
		defer source.mu.Unlock()
		row := source.panes[id]
		if assert.NotNil(collect, row.protocol) {
			assert.Equal(collect, protocols.Running, row.protocol.pane.Snapshot().Shell.Phase)
		}
		assert.NotEqual(collect, row.facts.Root, row.facts.Foreground)
		assert.NotEmpty(collect, row.pane.Scope.Connection)
	}, 5*time.Second, 10*time.Millisecond)
	sendShellInput(t, ctx, runner, id, "begin")
	require.Eventually(t, func() bool {
		snapshot, err := source.ReadStatus(ctx)
		return err == nil && snapshot.At(ref, time.Now()) == status.Working
	}, 3*time.Second, 10*time.Millisecond)
	sendShellInput(t, ctx, runner, id, "finish")
	require.Eventually(t, func() bool {
		source.mu.Lock()
		defer source.mu.Unlock()
		row := source.panes[id]
		return row.protocol.pane.Snapshot().Shell.Phase == protocols.Finished && row.facts.Foreground == row.facts.Root
	}, 5*time.Second, 10*time.Millisecond)
	source.mu.Lock()
	finished := source.panes[id].protocol.pane.Snapshot().Shell
	source.mu.Unlock()

	require.Equal(t, uint64(1), finished.Invocation)
	require.True(t, finished.ExitKnown)
	require.Equal(t, uint8(7), finished.ExitCode)
	sendShellInput(t, ctx, runner, id, "prompt")
	require.Eventually(t, func() bool {
		snapshot, err := source.ReadStatus(ctx)
		return err == nil && snapshot.At(ref, time.Now()) == status.Withdrawn
	}, 3*time.Second, 10*time.Millisecond)
}

func sendShellInput(t *testing.T, ctx context.Context, runner nativeRunner, pane, input string) {
	t.Helper()
	_, err := runner.RunLimited(ctx, 4096, "send-keys", "-t", pane, "-l", "--", input+"\n")
	require.NoError(t, err)
}

// Reads synchronize each boundary with the test, without timed shell sleeps or
// edits to user hooks. The child owns foreground while the parent waits for it.
const shellBoundaryFixture = `stty -echo
set -m
printf '\033]133;A\007\033]133;B\007'
while IFS= read -r command; do
    printf '\033]133;C\007'
    /bin/sh -c '
        IFS= read -r start || exit 1
        printf "\033]7501;state=working\007"
        IFS= read -r finish || exit 1
        exit 7
    '
    result=$?
    printf '\033]133;D;%s\007' "$result"
    IFS= read -r ack || exit 1
    printf '\033]133;A;cl=line\007\033]133;B\007'
done
`
