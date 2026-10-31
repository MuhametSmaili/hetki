package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestPickerFallbackThroughBoundedProcesses(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure string
		calls   int
	}{
		{"metadata unavailable", "", 2},
		{"combined overflow", "combined", 1},
		{"fallback overflow", "fallback", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			counter := filepath.Join(directory, "calls")
			script := `#!/bin/sh
printf x >> "$HETKI_TEST_QUERY_COUNT"
case "$1" in
    display-message)
        printf 'unavailable\n'
        if [ "$HETKI_TEST_QUERY_FAILURE" = combined ]; then
            printf '%5000s' x >&2
        fi
        ;;
    *)
        if [ "$HETKI_TEST_QUERY_FAILURE" = fallback ]; then
            printf '%5000s' x >&2
        fi
        ;;
esac
printf '0\n0\n$1|dev|@1|shell|0|layout|0|1|%%1|0|1|/fixture|cat||1|1|/bin/sh\n'
`
			require.NoError(t, os.WriteFile(filepath.Join(directory, "tmux"), []byte(script), 0700))
			t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TMUX", "")
			t.Setenv("TMUX_PANE", "")
			t.Setenv("HETKI_TEST_QUERY_COUNT", counter)
			t.Setenv("HETKI_TEST_QUERY_FAILURE", test.failure)
			b, err := tmux.NewBackend()
			require.NoError(t, err)
			adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)

			snapshot, loadErr := adapter.Load(t.Context(), ui.ViewFlat)

			calls, err := os.ReadFile(counter)
			require.NoError(t, err)
			require.Equal(t, strings.Repeat("x", test.calls), string(calls))
			if test.failure != "" {
				require.ErrorIs(t, loadErr, backend.ErrOutputLimitExceeded)
				require.Empty(t, snapshot.Items)
				return
			}
			require.NoError(t, loadErr)
			require.Len(t, snapshot.Items, 1)
			require.Equal(t, "shell", snapshot.Items[0].Primary)
			require.Equal(t, destinationItemID("$1", "@1", "%1"), snapshot.Items[0].ID)
			reader, statusErr := adapter.OpenStatus(t.Context())
			require.Nil(t, reader)
			require.Error(t, statusErr)
		})
	}
}
