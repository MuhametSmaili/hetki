package tmux

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type unsupportedBackendAction struct{}

func (unsupportedBackendAction) Comment() string { return "unsupported" }
func (unsupportedBackendAction) Validate() error { return nil }

func TestCanceledContextPreventsQueryDispatch(t *testing.T) {
	called := false
	b := &TmuxBackend{client: &MockClient{RunFunc: func(context.Context, ...string) (string, error) {
		called = true
		return "", nil
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := b.QueryState(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)
}

func TestQueryAndMutationObserveCancellation(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		started := make(chan struct{})
		b := &TmuxBackend{client: &MockClient{RunFunc: func(ctx context.Context, _ ...string) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		}}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := b.QueryState(ctx)
			done <- err
		}()
		awaitTmuxChannel(t, started)
		cancel()
		require.ErrorIs(t, awaitTmuxChannel(t, done), context.Canceled)
	})

	t.Run("mutation", func(t *testing.T) {
		started := make(chan struct{})
		b := &TmuxBackend{client: &MockClient{ExecuteFunc: func(ctx context.Context, _ Action) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- b.Apply(ctx, []backend.Action{backend.KillSessionAction{Name: "dev"}})
		}()
		awaitTmuxChannel(t, started)
		cancel()
		require.ErrorIs(t, awaitTmuxChannel(t, done), context.Canceled)
	})
}

func TestUnsupportedBackendActionFailsApplyAndDryRunBeforeDispatch(t *testing.T) {
	dispatched := false
	b := &TmuxBackend{client: &MockClient{ExecuteFunc: func(context.Context, Action) error {
		dispatched = true
		return nil
	}}}
	actions := []backend.Action{backend.KillSessionAction{Name: "dev"}, unsupportedBackendAction{}}

	require.ErrorContains(t, b.Apply(context.Background(), actions), "unsupported backend action")
	assert.False(t, dispatched)
	_, err := b.DryRun(actions)
	assert.ErrorContains(t, err, "unsupported backend action")
}

func TestQueryStateToleratesEmptyServer(t *testing.T) {
	t.Setenv("TMUX", "")

	cases := []error{
		errors.New("tmux [list-panes -a] failed: exit status 1 (no current target)"),
		errors.New("tmux [list-panes -a] failed: exit status 1 (no server running on /tmp/tmux-1000/default)"),
	}
	for _, runErr := range cases {
		b := &TmuxBackend{client: &MockClient{
			RunFunc: func(_ context.Context, args ...string) (string, error) {
				return "0\n0\n", runErr
			},
		}}
		res, err := b.QueryState(context.Background())
		require.NoError(t, err)
		assert.Empty(t, res.Sessions)
	}
}

func TestQueryStateDoesNotSuppressCancellationFromEmptyServer(t *testing.T) {
	sh, err := exec.LookPath("sh")
	require.NoError(t, err)
	err = exec.CommandContext(t.Context(), sh, "-c", "kill -INT $$").Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)

	for _, test := range []struct {
		name, output string
		cause        error
	}{
		{"caller cancellation", "0\n0\n", context.Canceled},
		{"child interrupted first", "partial", &backend.CanceledProcessError{Cause: exitErr}},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &TmuxBackend{client: &MockClient{RunFunc: func(context.Context, ...string) (string, error) {
				return test.output, errors.Join(test.cause, errors.New("no server running on /tmp/tmux/default"))
			}}}

			_, err := b.QueryState(t.Context())

			require.ErrorIs(t, err, test.cause)
		})
	}
}

func TestQueryStateDoesNotSuppressParseFailureFromEmptyServer(t *testing.T) {
	for _, output := range []string{"", "malformed"} {
		b := &TmuxBackend{client: &MockClient{RunFunc: func(context.Context, ...string) (string, error) {
			return output, errors.New("no server running on /tmp/tmux/default")
		}}}

		_, err := b.QueryState(context.Background())

		require.ErrorContains(t, err, "missing base indexes")
		assert.ErrorContains(t, err, "no server running")
	}
}

func TestQueryStatePropagatesRealFailures(t *testing.T) {
	t.Setenv("TMUX", "")

	for _, output := range []string{
		"0\n0",
		"0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|",
	} {
		b := &TmuxBackend{client: &MockClient{
			RunFunc: func(_ context.Context, args ...string) (string, error) {
				return output, errors.New("permission denied")
			},
		}}
		_, err := b.QueryState(context.Background())
		assert.ErrorContains(t, err, "permission denied")
	}
}

func TestQueryStatePreservesStableObjectIDsAndPaneIndex(t *testing.T) {
	t.Setenv("TMUX", ",,1")
	b := &TmuxBackend{client: &MockClient{RunFunc: func(_ context.Context, args ...string) (string, error) {
		return "0\n1\n$1|dev|@2|editor|3|layout-a|0|1|%7|4|1|~/code|vim||100|0|/bin/sh", nil
	}}}

	result, err := b.QueryState(context.Background())
	require.NoError(t, err)
	if assert.Len(t, result.Sessions, 1) && assert.Len(t, result.Sessions[0].Windows, 1) && assert.Len(t, result.Sessions[0].Windows[0].Panes, 1) {
		assert.Equal(t, "/bin/sh", result.Sessions[0].DefaultShell)
		assert.Equal(t, "$1", result.Sessions[0].ID)
		assert.Equal(t, "@2", result.Sessions[0].Windows[0].ID)
		assert.Equal(t, "%7", result.Sessions[0].Windows[0].Panes[0].ID)
		assert.Equal(t, 4, result.Sessions[0].Windows[0].Panes[0].Index)
		assert.Equal(t, backend.ActiveContext{SessionID: "$1", Session: "dev", WindowID: "@2", Window: "editor", WindowIndex: 3, PaneID: "%7", Pane: 4, Path: "~/code"}, result.Active)
	}
}

func TestApplyRejectsWindowDestructionWithoutStableID(t *testing.T) {
	executed := false
	b := &TmuxBackend{client: &MockClient{ExecuteFunc: func(context.Context, Action) error {
		executed = true
		return nil
	}}}

	err := b.Apply(context.Background(), []backend.Action{backend.KillWindowAction{Session: "dev", Window: "editor"}})
	require.ErrorContains(t, err, "stable ID")
	assert.False(t, executed)
}

func TestApplyRejectsMalformedStableWindowIDs(t *testing.T) {
	for _, action := range []backend.Action{
		backend.RenameWindowAction{Session: "dev", Window: "editor", WindowID: "@bad", New: "logs"},
		backend.KillWindowAction{Session: "dev", Window: "editor", WindowID: "2"},
	} {
		executed := false
		b := &TmuxBackend{client: &MockClient{ExecuteFunc: func(context.Context, Action) error {
			executed = true
			return nil
		}}}

		err := b.Apply(context.Background(), []backend.Action{action})

		require.ErrorContains(t, err, "invalid window ID")
		assert.False(t, executed)
	}
}

func TestCreateTargetRejectsMalformedReturnedIDs(t *testing.T) {
	for _, output := range []string{"@bad|%1", "@1|%bad"} {
		b := &TmuxBackend{client: &MockClient{RunFunc: func(context.Context, ...string) (string, error) {
			return output, nil
		}}}

		err := b.Apply(context.Background(), []backend.Action{backend.CreateWindowAction{Session: "dev", Name: "logs"}})

		assert.ErrorContains(t, err, "invalid created target")
	}
}

func TestApplyRejectsMalformedReturnedSplitPaneID(t *testing.T) {
	b := &TmuxBackend{client: &MockClient{RunFunc: func(_ context.Context, args ...string) (string, error) {
		if args[0] == "new-session" {
			return "@1|%1", nil
		}
		return "%bad", nil
	}}}

	err := b.Apply(context.Background(), []backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor"},
		backend.SplitPaneAction{Session: "dev", Window: "editor"},
	})

	assert.ErrorContains(t, err, "invalid pane ID")
}

func TestApplyUsesReturnedCreationIDsForFollowups(t *testing.T) {
	var events []string
	client := &MockClient{
		RunFunc: func(_ context.Context, args ...string) (string, error) {
			events = append(events, "run "+strings.Join(args, " "))
			switch args[0] {
			case "new-session":
				return "@10|%20", nil
			case "new-window":
				return "@11|%30", nil
			case "split-window":
				return "%21", nil
			default:
				return "", nil
			}
		},
		ExecuteFunc: func(_ context.Context, action Action) error {
			events = append(events, "exec "+strings.Join(action.Args(), " "))
			return nil
		},
	}
	b := &TmuxBackend{client: client}
	actions := []backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor", Path: "~/code"},
		backend.SplitPaneAction{Session: "dev", Window: "editor", Path: "~/api"},
		backend.SelectLayoutAction{Session: "dev", Window: "editor", Layout: "tiled"},
		backend.SendKeysAction{Session: "dev", Window: "editor", Pane: 1, Command: "npm test"},
		backend.ZoomPaneAction{Session: "dev", Window: "editor", Pane: 1},
		backend.CreateWindowAction{Session: "dev", Name: "server", Path: "~/srv"},
		backend.RenameWindowAction{Session: "dev", Window: "server", WindowID: "@11", New: "logs"},
		backend.KillWindowAction{Session: "dev", Window: "logs", WindowID: "@11"},
	}

	require.NoError(t, b.Apply(context.Background(), actions))
	assert.Equal(t, []string{
		"run new-session -d -s dev -n editor -c ~/code -P -F #{window_id}|#{pane_id}",
		"run split-window -t @10 -c ~/api -P -F #{pane_id}",
		"exec select-layout -t @10 tiled",
		"exec send-keys -l -t %21 -- npm test ; send-keys -t %21 Enter",
		"exec resize-pane -Z -t %21",
		"run new-window -t dev: -n server -c ~/srv -P -F #{window_id}|#{pane_id}",
		"exec rename-window -t @11 logs",
		"exec kill-window -t @11",
	}, events)
}

func TestDryRunUsesSymbolicCreatedIDsInsteadOfPredictedIndexes(t *testing.T) {
	b := &TmuxBackend{}
	lines, err := b.DryRun([]backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor"},
		backend.SplitPaneAction{Session: "dev", Window: "editor", Path: "~/api"},
		backend.SendKeysAction{Session: "dev", Window: "editor", Pane: 1, Command: "npm test"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"tmux new-session -d -s dev -n editor -P -F #{window_id}|#{pane_id}",
		"tmux split-window -t <new-window:dev:editor> -c ~/api -P -F #{pane_id}",
		"tmux send-keys -l -t <new-pane:dev:editor:1> -- npm test ; send-keys -t <new-pane:dev:editor:1> Enter",
	}, lines)
}

func TestDryRunDetachedCreation(t *testing.T) {
	b := &TmuxBackend{}
	actions := []backend.Action{
		backend.CreateWindowAction{Session: "dev", Name: "server", Path: "~/api", Detached: true},
		backend.SplitPaneAction{Session: "dev", Window: "server", Path: "~/tools", Detached: true},
		backend.SplitPaneAction{Session: "dev", Window: "server", Path: "~/tests", Detached: true},
		backend.SendKeysAction{Session: "dev", Window: "server", Pane: 2, Command: "npm test"},
	}

	lines, err := b.DryRun(actions)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"tmux new-window -t dev: -n server -d -c ~/api -P -F #{window_id}|#{pane_id}",
		"tmux split-window -t <new-pane:dev:server:0> -d -c ~/tools -P -F #{pane_id}",
		"tmux split-window -t <new-pane:dev:server:1> -d -c ~/tests -P -F #{pane_id}",
		"tmux send-keys -l -t <new-pane:dev:server:2> -- npm test ; send-keys -t <new-pane:dev:server:2> Enter",
	}, lines)
}

func BenchmarkSwitchStablePaneTenThousandPanes(b *testing.B) {
	b.Setenv("TMUX", "")
	var output strings.Builder
	output.WriteString("0\n0\n")
	for i := range 10_000 {
		active := 0
		if i == 9_999 {
			active = 1
		}
		fmt.Fprintf(&output, "$1|dev|@1|editor|0|layout|0|1|%%%d|%d|%d|/work/%d|sh|\n", i, i, active, i)
	}
	stateOutput := strings.TrimSuffix(output.String(), "\n")
	adapter := &TmuxBackend{client: &MockClient{RunFunc: func(context.Context, ...string) (string, error) {
		return stateOutput, nil
	}}}

	b.ReportAllocs()
	for b.Loop() {
		if err := adapter.Switch(context.Background(), backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%9999"}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSwitchResolvesStableOccurrences(t *testing.T) {
	stateOutput := "0\n1\n" +
		"$1|core|@1|editor|3|layout|0|1|%1|4|1|/p|vim|\n" +
		"$1|core|@2|logs.0|5|layout|0|0|%2|2|1|/p|sh|\n" +
		"$2|$1|@1|editor|9|layout|0|1|%1|4|1|/p|vim|\n" +
		"$3|a:b|@3|w|0|layout|0|1|%3|0|1|/p|sh|"
	tests := []struct {
		name    string
		target  backend.Target
		wantNav string
	}{
		{name: "session ID ignores name shadow", target: backend.Target{SessionID: "$1"}, wantNav: "$1"},
		{name: "window uses current index", target: backend.Target{SessionID: "$1", WindowID: "@1"}, wantNav: "$1:3"},
		{name: "pane uses current index without base offset", target: backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"}, wantNav: "$1:3.4"},
		{name: "linked window keeps session", target: backend.Target{SessionID: "$2", WindowID: "@1"}, wantNav: "$2:9"},
		{name: "linked pane keeps occurrence", target: backend.Target{SessionID: "$2", WindowID: "@1", PaneID: "%1"}, wantNav: "$2:9.4"},
		{name: "dotted window name", target: backend.Target{SessionID: "$1", WindowID: "@2"}, wantNav: "$1:5"},
		{name: "colon session name", target: backend.Target{SessionID: "$3", WindowID: "@3"}, wantNav: "$3:0"},
		{name: "missing session", target: backend.Target{SessionID: "$99", WindowID: "@1"}},
		{name: "missing window", target: backend.Target{SessionID: "$1", WindowID: "@99"}},
		{name: "missing pane", target: backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%99"}},
		{name: "window in another session", target: backend.Target{SessionID: "$3", WindowID: "@1"}},
		{name: "pane in another window", target: backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%2"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TMUX", "")
			var nav []string
			queries := 0
			b := &TmuxBackend{client: &MockClient{
				RunFunc: func(context.Context, ...string) (string, error) {
					queries++
					return stateOutput, nil
				},
				ExecuteFunc: func(_ context.Context, action Action) error {
					nav = append(nav, strings.Join(action.Args(), " "))
					return nil
				},
			}}

			err := b.Switch(context.Background(), test.target)

			if test.wantNav == "" {
				require.Error(t, err)
				assert.Empty(t, nav)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []string{"attach-session -t " + test.wantNav}, nav)
			if test.target.WindowID == "" {
				assert.Zero(t, queries, "session IDs need no inventory lookup")
			} else {
				assert.Equal(t, 1, queries)
			}
		})
	}
}

func TestSwitchPropagatesQueryFailureWithoutNavigation(t *testing.T) {
	failure := errors.New("inventory unavailable")
	for _, queryErr := range []error{failure, context.Canceled, context.DeadlineExceeded} {
		t.Run(queryErr.Error(), func(t *testing.T) {
			b := &TmuxBackend{client: &MockClient{
				RunFunc: func(context.Context, ...string) (string, error) {
					return "", queryErr
				},
				ExecuteFunc: func(context.Context, Action) error {
					t.Fatal("failed query must not navigate")
					return nil
				},
			}}

			err := b.Switch(context.Background(), backend.Target{SessionID: "$1", WindowID: "@1"})

			require.ErrorIs(t, err, queryErr)
		})
	}
}

func TestCanceledContextPreventsSwitchDispatch(t *testing.T) {
	b := &TmuxBackend{client: &MockClient{
		RunFunc: func(context.Context, ...string) (string, error) {
			t.Fatal("canceled switch must not query")
			return "", nil
		},
		ExecuteFunc: func(context.Context, Action) error {
			t.Fatal("canceled switch must not navigate")
			return nil
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, target := range []backend.Target{{SessionID: "$1"}, {SessionID: "$1", WindowID: "@1", PaneID: "%1"}} {
		require.ErrorIs(t, b.Switch(ctx, target), context.Canceled)
	}
}

func TestSessionSwitchAndAttachPreserveDispatchErrors(t *testing.T) {
	t.Setenv("TMUX", "isolated")
	failure := errors.New("session no longer exists")
	var targets []string
	b := &TmuxBackend{client: &MockClient{ExecuteFunc: func(_ context.Context, action Action) error {
		targets = append(targets, strings.Join(action.Args(), " "))
		return failure
	}}}

	switchErr := b.Switch(context.Background(), backend.Target{SessionID: "$1"})
	attachErr := b.Attach(context.Background(), "start-name")

	require.ErrorIs(t, switchErr, failure)
	require.ErrorIs(t, attachErr, failure)
	assert.Equal(t, []string{"switch-client -t $1", "switch-client -t start-name"}, targets)
}
