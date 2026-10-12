//go:build integration

package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/backend/tmux"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/daemon"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestNativePickerStartsReusesAndRebindsDaemon(t *testing.T) {
	b, client, run := pickerDaemonFixture(t)
	executable := filepath.Join(t.TempDir(), "hetki")
	build := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", executable, "../../..")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)

	runPickerPTY(t, executable, client)
	first, err := client.Ping(t.Context())
	require.NoError(t, err, "daemon must survive picker exit")
	adapter := newLiveAdapter(func(...string) (backend.Backend, error) { return b, nil }, nil, nil)
	adapter.projection = projectionTree
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (ui.BackendTarget, error) {
		require.NotNil(t, options.Read)
		idle := awaitPickerStatus(t, ctx, options, status.Idle)
		require.True(t, idle.Watching, "native daemon must use change delivery")
		require.Len(t, idle.Bindings, 2, "linked window occurrences share one pane")
		before := firstPaneRef(t, idle.Values)
		_, err := dispatch(ui.ActionRequest{ActionID: ui.ActionToggleProjection})
		require.NoError(t, err)
		flat := awaitPickerStatus(t, ctx, options, status.Idle)
		require.Contains(t, flat.Bindings, destinationItemID("$0", "@0", "%0"))
		run("send-keys", "-t", "%0", "-l", "--", "\x1b[2J\x1b[HAPPROVAL\n")
		awaitPickerStatus(t, ctx, options, status.Blocked)
		run("respawn-pane", "-k", "-t", "%0", "stty -echo; printf 'READY\n'; exec cat")
		require.Eventually(t, func() bool {
			update := options.Read(ctx, status.Snapshot{})
			for ref := range update.Values.All() {
				if ref.ID == before.ID && ref.Generation != before.Generation && update.Values.At(ref, time.Now()) == status.Idle {
					return update.Bindings.StateAt(destinationItemID("$0", "@0", "%0"), update.Values, time.Now()) == status.Idle
				}
			}
			return false
		}, 5*time.Second, 20*time.Millisecond)
		return "", nil
	}

	require.NoError(t, service.Run(t.Context()))
	second, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func firstPaneRef(t *testing.T, values status.Snapshot) status.PaneRef {
	t.Helper()
	for ref := range values.All() {
		return ref
	}
	t.Fatal("expected a reported pane")
	return status.PaneRef{}
}

func awaitPickerStatus(t *testing.T, ctx context.Context, options ui.StatusOptions, want status.State) ui.StatusUpdate {
	t.Helper()
	var update ui.StatusUpdate
	require.Eventually(t, func() bool {
		update = options.Read(ctx, status.Snapshot{})
		if len(update.Bindings) == 0 {
			return false
		}
		for id := range update.Bindings {
			if update.Bindings.StateAt(id, update.Values, time.Now()) != want {
				return false
			}
		}
		return true
	}, 5*time.Second, 20*time.Millisecond)
	return update
}

func pickerDaemonFixture(t testing.TB) (*tmux.TmuxBackend, *daemon.Client, func(...string) string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required")
	}
	dir, err := os.MkdirTemp("", "hetki-ui-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	socket := filepath.Join(dir, "tmux.sock")
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	run := func(args ...string) string {
		command := exec.CommandContext(t.Context(), "tmux", append([]string{"-S", socket, "-f", "/dev/null"}, args...)...)
		out, err := command.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	run("new-session", "-d", "-s", "first", "-x", "80", "-y", "24", "stty -echo; printf 'READY\n'; exec cat")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "tmux", "-S", socket, "kill-server").CombinedOutput()
		require.NoError(t, err, "%s", out)
	})
	pid := run("display-message", "-p", "#{pid}")
	t.Setenv("TMUX", fmt.Sprintf("%s,%s,0", socket, pid))
	run("new-session", "-d", "-s", "linked", "-t", "first")
	configDir := filepath.Join(dir, "hetki", "status.d")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "cat.yaml"), []byte(`version: 2
id: fixture
match: {command: cat}
source:
  type: screen
  screen:
    poll_interval: 2s
    region: {last_nonblank_rows: 12}
    rules:
      - state: idle
        all: [{contains: READY}]
      - state: blocked
        all: [{contains: APPROVAL}]
`), 0600))
	b, err := tmux.NewBackend()
	require.NoError(t, err)
	server, err := b.StatusServer(t.Context())
	require.NoError(t, err)
	client, err := daemon.Connect(server.Identity())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		require.NoError(t, client.Stop(ctx))
		require.Eventually(t, func() bool {
			_, err := client.Ping(ctx)
			return err != nil
		}, 3*time.Second, 20*time.Millisecond)
	})
	return b, client, run
}

type pickerOutput struct {
	mu    sync.Mutex
	data  bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (w *pickerOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.data.Write(data)
	if bytes.Contains(w.data.Bytes(), []byte("\x1b[?1049h")) {
		w.once.Do(func() { close(w.ready) })
	}
	return n, err
}

func runPickerPTY(t *testing.T, executable string, client *daemon.Client) {
	t.Helper()
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script is required for picker startup coverage")
	}
	args := []string{"-q", "/dev/null", executable}
	if runtime.GOOS != "darwin" {
		args = []string{"-q", "-e", "-c", executable, "/dev/null"}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "script", args...)
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output := &pickerOutput{ready: make(chan struct{})}
	command.Stdout, command.Stderr = output, output
	require.NoError(t, command.Start())
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()
	select {
	case <-output.ready:
	case err := <-done:
		t.Fatalf("picker exited before entering terminal UI: %v", err)
	case <-ctx.Done():
		t.Fatal("picker did not enter terminal UI")
	}
	require.Eventually(t, func() bool {
		_, err := client.Ping(ctx)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond, "asynchronous picker read must ensure the daemon")
	_, err = input.Write([]byte{3})
	require.NoError(t, err)
	require.NoError(t, awaitChannel(t, done))
}
