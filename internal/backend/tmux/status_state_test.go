package tmux

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func TestStatusStateBatchesRowsInsideServerValidation(t *testing.T) {
	t.Setenv("TMUX", "")
	metadata := fmt.Sprintf("/fixture/socket|%d|123|3.8\n", os.Getpid())
	rows := "0\n0\n$1|dev|@1|shell|0|layout|0|1|%1|0|1|/fixture|cat||1|1|/bin/sh\n"
	calls := 0
	b := &TmuxBackend{client: &MockClient{RunLimitedFunc: func(_ context.Context, limit int, args ...string) (string, error) {
		calls++
		if calls == 1 {
			require.NotContains(t, args, "list-panes")
			return metadata, nil
		}
		require.Contains(t, args, "list-panes")
		require.LessOrEqual(t, len(metadata)+len(rows), limit)
		return metadata + rows, nil
	}}}

	state, server, err := b.QueryStatusState(t.Context())

	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, os.Getpid(), server.PID)
	require.NotEmpty(t, server.Birth)
	require.Len(t, state.Sessions, 1)
	require.Equal(t, "/bin/sh", state.Sessions[0].DefaultShell)
	require.Equal(t, "%1", state.Sessions[0].Windows[0].Panes[0].ID)
}

func TestStatusStateRejectsChangedOrIncompleteAcquisition(t *testing.T) {
	t.Setenv("TMUX", "")
	metadata := fmt.Sprintf("/fixture/socket|%d|123|3.8\n", os.Getpid())
	for name, second := range map[string]string{
		"changed server":     strings.Replace(metadata, "|123|", "|124|", 1) + "0\n0\n",
		"missing separator":  strings.TrimSuffix(metadata, "\n"),
		"oversized metadata": strings.Repeat("x", 8193) + "\n0\n0\n",
		"invalid rows":       metadata + "not a picker inventory",
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			b := &TmuxBackend{client: &MockClient{RunLimitedFunc: func(context.Context, int, ...string) (string, error) {
				calls++
				if calls == 1 {
					return metadata, nil
				}
				return second, nil
			}}}

			state, server, err := b.QueryStatusState(t.Context())

			require.Error(t, err)
			require.Empty(t, state.Sessions)
			require.Empty(t, server)
			if name == "changed server" {
				require.ErrorIs(t, err, observe.ErrServerChanged)
			}
		})
	}
}
