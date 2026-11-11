package tmux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionStateDistinguishesAbsenceFromQueryFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		stderr string
		stdout string
		absent bool
	}{
		{"missing socket", "error connecting to /fixture/socket (No such file or directory)\n", "", true},
		{"stale socket", "no server running on /fixture/socket\n", "", true},
		{"permission denied", "error connecting to /fixture/socket (Permission denied)\n", "", false},
		{"server crash", "server exited unexpectedly\n", "", false},
		{"partial query", "no server running on /fixture/socket\n", "partial", false},
		{"extra diagnostics", "no server running on /fixture/socket\nanother failure\n", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TMUX", "")
			t.Setenv("TMUX_PANE", "")
			wrapper := filepath.Join(t.TempDir(), "tmux")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%b' %q\nprintf '%%b' %q >&2\nexit 1\n", test.stdout, test.stderr)
			require.NoError(t, os.WriteFile(wrapper, []byte(script), 0700))
			b := &TmuxBackend{client: &client{bin: wrapper}}

			snapshot, err := b.QueryActionState(t.Context())

			if !test.absent {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Nil(t, snapshot.Server)
			assert.Nil(t, snapshot.State.Invocation)
			assert.Equal(t, "/fixture/socket", snapshot.Socket)
		})
	}
}

func TestActionStatePreservesCanceledAcquisition(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	b := &TmuxBackend{client: &client{bin: "tmux"}}

	_, err := b.QueryActionState(ctx)

	require.ErrorIs(t, err, context.Canceled)
}
