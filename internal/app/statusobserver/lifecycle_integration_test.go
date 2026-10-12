//go:build integration

package statusobserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/screen"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestNativePromptWithdrawalSurvivesForegroundRefresh(t *testing.T) {
	runner, server, sampler, id, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	startLifecycleShell(t, ctx, runner, id, shellBoundaryFixture)
	var pause atomic.Bool
	reached, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	var gate sync.Once
	selectProfile := func(facts observe.Pane) Selection {
		if facts.ID == id && pause.Load() && facts.Foreground == facts.Root {
			gate.Do(func() {
				close(reached)
				select {
				case <-release:
				case <-ctx.Done():
				}
			})
		}
		return Selection{}
	}
	source, err := NewSource(ctx, server, sampler, selectProfile, func(err error) {
		t.Errorf("unexpected source error: %v", err)
	})
	require.NoError(t, err)
	defer func() {
		unblock()
		require.NoError(t, source.Close())
	}()
	ref := awaitSourcePane(t, source, id, "")
	sendShellInput(t, ctx, runner, id, "run")
	awaitLifecycleForeground(t, source, id, true)
	sendShellInput(t, ctx, runner, id, "begin")
	awaitLifecycleState(t, source, ref, status.Working)
	pause.Store(true)
	sendShellInput(t, ctx, runner, id, "finish")
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatal("inventory did not reach the returning shell")
	}

	// The prompt arrives on the old stream scope before staged native facts publish.
	sendShellInput(t, ctx, runner, id, "prompt")
	awaitLifecycleState(t, source, ref, status.Withdrawn)
	unblock()
	awaitLifecycleForeground(t, source, id, false)
	refreshLifecycleSource(t, source)

	requireSourceState(t, source, ref, status.Withdrawn)
}

func TestNativeScreenProfileReleaseReturnsShellToUnreported(t *testing.T) {
	runner, server, sampler, id, _ := nativeStreamsFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	script := strings.Replace(shellBoundaryFixture, `printf "\033]7501;state=working\007"`, `printf "WORKING\n"`, 1)
	startLifecycleShell(t, ctx, runner, id, script)
	evaluator, err := screen.Compile(screen.Config{
		LastNonblankRows: 4,
		Rules: []screen.Rule{{
			State: status.Working,
			All:   []screen.Matcher{{Contains: "WORKING"}},
		}},
	})
	require.NoError(t, err)
	selectProfile := func(facts observe.Pane) Selection {
		if facts.Foreground.Identity() == "" || facts.Foreground == facts.Root {
			return Selection{}
		}
		return Selection{
			Profile:  service.Profile{Source: service.Screen, Generation: "child-fixture"},
			Evaluate: evaluator.Evaluate,
		}
	}
	source, err := NewSource(ctx, server, sampler, selectProfile, func(err error) {
		// A capture racing the child's exit must reject its old native facts.
		if !errors.Is(err, observe.ErrChanged) {
			t.Errorf("unexpected source error: %v", err)
		}
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Close()) }()
	ref := awaitSourcePane(t, source, id, "")
	sendShellInput(t, ctx, runner, id, "run")
	awaitLifecycleForeground(t, source, id, true)
	sendShellInput(t, ctx, runner, id, "begin")
	awaitLifecycleState(t, source, ref, status.Working)

	sendShellInput(t, ctx, runner, id, "finish")
	awaitLifecycleForeground(t, source, id, false)
	sendShellInput(t, ctx, runner, id, "prompt")
	refreshLifecycleSource(t, source)

	requireSourceState(t, source, ref, status.Unreported)
}

func startLifecycleShell(t *testing.T, ctx context.Context, runner nativeRunner, id, content string) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "lifecycle.sh")
	require.NoError(t, os.WriteFile(script, []byte(content), 0o600))
	_, err := runner.RunLimited(ctx, 4096, "respawn-pane", "-k", "-t", id, fmt.Sprintf("exec /bin/sh %q", script))
	require.NoError(t, err)
}

func awaitLifecycleForeground(t *testing.T, source *Source, id string, child bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		source.mu.Lock()
		defer source.mu.Unlock()
		row := source.panes[id]
		return row.pane.Scope.Connection != "" && (row.facts.Foreground != row.facts.Root) == child
	}, 5*time.Second, 10*time.Millisecond)
}

func awaitLifecycleState(t *testing.T, source *Source, ref status.PaneRef, state status.State) {
	t.Helper()
	require.Eventually(t, func() bool {
		snapshot, err := source.ReadStatus(t.Context())
		return err == nil && snapshot.At(ref, time.Now()) == state
	}, 3*time.Second, 10*time.Millisecond)
}

func refreshLifecycleSource(t *testing.T, source *Source) {
	t.Helper()
	source.mu.Lock()
	before := source.nativeRevision
	source.mu.Unlock()
	source.streams.signalChange()
	require.Eventually(t, func() bool {
		source.mu.Lock()
		defer source.mu.Unlock()
		return source.nativeRevision > before
	}, 3*time.Second, 10*time.Millisecond)
}
