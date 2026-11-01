package tmux

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwitchRejectsReplacedClientConnection(t *testing.T) {
	for _, replacement := range []struct{ name, row string }{
		{"different process", "client|$1|%1||0|owner|101|1\n"},
		{"new connection", "client|$1|%1||0|owner|100|2\n"},
	} {
		t.Run(replacement.name, func(t *testing.T) {
			t.Setenv("TMUX", ",,1")
			t.Setenv("TMUX_PANE", "%1")
			rows := "client|$1|%1||0|owner|100|1\n"
			dispatched := 0
			b := &TmuxBackend{client: &MockClient{
				RunLimitedFunc: func(context.Context, int, ...string) (string, error) {
					return rows, nil
				},
				ExecuteFunc: func(context.Context, Action) error {
					dispatched++
					return nil
				},
			}}
			target := backend.Target{SessionID: "$2"}
			require.NoError(t, b.Switch(t.Context(), target))
			rows = replacement.row

			err := b.Switch(t.Context(), target)

			require.ErrorContains(t, err, "no longer attached")
			assert.Equal(t, 1, dispatched, "replacement connection must not receive navigation")
		})
	}
}

func TestClientDiscoveryFailurePreventsSessionNavigation(t *testing.T) {
	for _, failure := range []error{errors.New("client query unavailable"), context.Canceled, context.DeadlineExceeded, backend.ErrOutputLimitExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Setenv("TMUX", ",,1")
			t.Setenv("TMUX_PANE", "%1")
			b := &TmuxBackend{client: &MockClient{
				RunLimitedFunc: func(ctx context.Context, limit int, args ...string) (string, error) {
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					require.LessOrEqual(t, time.Until(deadline), queryTimeout)
					require.Equal(t, stateOutputLimit, limit)
					require.Equal(t, "list-clients", args[0])
					return "client|$1|%1||0|owner|100|1\n", failure
				},
				ExecuteFunc: func(context.Context, Action) error {
					t.Fatal("failed discovery must not dispatch navigation")
					return nil
				},
			}}

			switchErr := b.Switch(t.Context(), backend.Target{SessionID: "$2"})
			attachErr := b.Attach(t.Context(), "destination")

			require.ErrorIs(t, switchErr, failure)
			require.ErrorIs(t, attachErr, failure)
		})
	}
}

func TestClientDiscoveryRejectsMalformedOutputBeforeNavigation(t *testing.T) {
	for _, invalid := range []struct{ name, row string }{
		{"malformed record", "malformed\n"},
		{"invalid PID", "client|$1|%1||0|owner|not-a-pid|1\n"},
		{"negative creation", "client|$1|%1||0|owner|100|-1\n"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			t.Setenv("TMUX", ",,1")
			t.Setenv("TMUX_PANE", "%1")
			b := &TmuxBackend{client: &MockClient{
				RunLimitedFunc: func(context.Context, int, ...string) (string, error) {
					return "client|$1|%1||0|owner|100|1\n" + invalid.row, nil
				},
				ExecuteFunc: func(context.Context, Action) error {
					t.Fatal("a valid prefix must not hide malformed discovery output")
					return nil
				},
			}}

			err := b.Switch(t.Context(), backend.Target{SessionID: "$2"})

			require.ErrorContains(t, err, "invalid tmux client row")
		})
	}
}
