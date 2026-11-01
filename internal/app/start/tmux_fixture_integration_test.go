//go:build integration

package start

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
)

// newIsolatedStartBackend uses a controlled shell/config and records navigation
// instead of attaching to the test runner's terminal. The second result is its log.
func newIsolatedStartBackend(t *testing.T) (*backendtmux.TmuxBackend, string) {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is required for integration tests")
	}

	socketDir := testenv.SocketDir(t)
	socket := filepath.Join(socketDir, "tmux.sock")
	navigationLog := filepath.Join(socketDir, "navigation.log")
	config := filepath.Join(socketDir, "tmux.conf")
	configText := `set -g default-shell /bin/sh
set -g default-command "exec /bin/sh"
set -g base-index 1
set -g pane-base-index 1
set -g automatic-rename off
`
	require.NoError(t, os.WriteFile(config, []byte(configText), 0600))

	wrapperDir := t.TempDir()
	wrapper := fmt.Sprintf(`#!/bin/sh
case "$1" in attach-session|switch-client) printf '%%s\n' "$*" >> %q; exit 0;; esac
if [ "$1" = "$HETKI_TEST_FAIL_COMMAND" ] && { [ -z "$HETKI_TEST_FAIL_TARGET" ] || [ "$3" = "$HETKI_TEST_FAIL_TARGET" ]; }; then
  printf 'injected failure: %%s\n' "$*" >&2
  exit 73
fi
exec %q -S %q -f %q "$@"
`, navigationLog, realTmux, socket, config)
	require.NoError(t, os.WriteFile(filepath.Join(wrapperDir, "tmux"), []byte(wrapper), 0700))
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "")
	t.Setenv("BASH_ENV", "")
	t.Setenv("HETKI_TEST_FAIL_COMMAND", "")
	t.Setenv("HETKI_TEST_FAIL_TARGET", "")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		output, err := exec.CommandContext(ctx, realTmux, "-S", socket, "kill-server").CombinedOutput()
		if err != nil {
			t.Errorf("clean up isolated tmux server: %v: %s", err, output)
		}
	})

	b, err := backendtmux.NewBackend()
	require.NoError(t, err)
	return b, navigationLog
}

// drainShellInput queues an acknowledgement after earlier input. Use it only
// with the fixture's foreground shell, not an interactive application.
func drainShellInput(t *testing.T, ctx context.Context, paneID string) {
	t.Helper()
	channel := "hetki-input-drained-" + strings.TrimPrefix(paneID, "%")
	output, err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", paneID,
		"-l", "tmux wait-for -S "+channel, ";", "send-keys", "-t", paneID, "Enter").CombinedOutput()
	require.NoError(t, err, "queue shell acknowledgement for %s: %s", paneID, output)

	output, err = exec.CommandContext(ctx, "tmux", "wait-for", channel).CombinedOutput()
	require.NoError(t, err, "drain shell input for %s: %s", paneID, output)
}
