package tmux

import (
	"context"
	"errors"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/require"
)

func TestLimitedStateQueryPreservesAcquisitionFailures(t *testing.T) {
	t.Setenv("TMUX", "")
	emptyServer := errors.New("no server running on /fixture/socket")
	denied := errors.New("permission denied")
	overflow := errors.Join(backend.ErrOutputLimitExceeded, emptyServer)
	for _, test := range []struct {
		name         string
		output       string
		runErr       error
		wantErr      error
		wantParseErr bool
	}{
		{"empty server", "0\n0\n", emptyServer, nil, false},
		{"malformed inventory", "invalid", denied, denied, true},
		{"overflow with valid-looking empty prefix", "0\n0\n", overflow, backend.ErrOutputLimitExceeded, false},
		{"overflow with malformed prefix", "invalid", overflow, backend.ErrOutputLimitExceeded, false},
		{"deadline with partial output", "invalid", errors.Join(context.DeadlineExceeded, emptyServer), context.DeadlineExceeded, false},
		{"cancellation with diagnostics", "invalid", errors.Join(context.Canceled, denied), context.Canceled, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := &TmuxBackend{client: &MockClient{RunLimitedFunc: func(context.Context, int, ...string) (string, error) {
				return test.output, test.runErr
			}}}

			state, err := b.QueryStateLimited(t.Context())

			require.ErrorIs(t, err, test.wantErr)
			require.Empty(t, state.Sessions)
			var parseErr *queryParseError
			if test.wantParseErr {
				require.ErrorAs(t, err, &parseErr)
			} else {
				require.NotErrorAs(t, err, &parseErr, "incomplete acquisition must not be parsed")
			}
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.runErr, "retain the original acquisition diagnostics")
			}
		})
	}
}

func TestLimitedStateQueryBoundsAcquisition(t *testing.T) {
	t.Setenv("TMUX", "")
	for _, test := range []struct {
		name     string
		rows     string
		sessions int
	}{
		{"populated", "0\n0\n$1|dev|@1|shell|0|layout|0|1|%1|0|1|/fixture|cat||1|1|/bin/sh\n", 1},
		{"empty", "0\n0\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			b := &TmuxBackend{client: &MockClient{
				RunFunc: func(context.Context, ...string) (string, error) {
					t.Fatal("bounded state query must not use Run")
					return "", nil
				},
				RunLimitedFunc: func(ctx context.Context, limit int, args ...string) (string, error) {
					calls++
					require.Equal(t, 16<<20, limit)
					require.Contains(t, args, "list-panes")
					_, hasDeadline := ctx.Deadline()
					require.True(t, hasDeadline)
					return test.rows, nil
				},
			}}

			state, err := b.QueryStateLimited(t.Context())

			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Len(t, state.Sessions, test.sessions)
			if test.sessions > 0 {
				require.Equal(t, "$1", state.Sessions[0].ID)
				require.Equal(t, "/bin/sh", state.Sessions[0].DefaultShell)
				require.Equal(t, "%1", state.Sessions[0].Windows[0].Panes[0].ID)
			}
		})
	}
}
