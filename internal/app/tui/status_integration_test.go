//go:build integration

package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestNativePickerStartsReusesAndRebindsDaemon(t *testing.T) {
	executable := buildPickerExecutable(t)
	b, client, run := pickerDaemonFixture(t)

	runPickerPTY(t, executable, client)
	first, err := client.Ping(t.Context())
	require.NoError(t, err, "daemon must survive picker exit")
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	adapter.projection = projectionTree
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, dispatch ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		require.NotNil(t, options.Read)
		idle := awaitPickerValue(t, ctx, options, status.Value{State: status.Idle}, nil)
		require.True(t, idle.Watching, "native daemon must use change delivery")
		require.Len(t, idle.Bindings, 2, "linked window occurrences share one pane")
		before := firstPaneRef(t, idle.Values)
		for _, id := range []list.ItemID{destinationItemID("$0", "@0", "%0"), windowItemID("$0", "@0")} {
			result, err := dispatch(ui.ActionRequest{ActionID: ui.ActionToggleProjection})
			require.NoError(t, err)
			require.NotNil(t, result.Status)
			require.Len(t, result.Status.Bindings, 2)
			require.Equal(t, status.Idle, result.Status.Bindings.StateAt(id, result.Status.Values, time.Now()), "replacement must carry status before another read")
			acceptStatusUpdate(*result.Status)
		}
		run("send-keys", "-t", "%0", "-l", "--", "\x1b[2J\x1b[HAPPROVAL\n")
		awaitPickerValue(t, ctx, options, status.Value{State: status.Blocked}, nil)
		run("respawn-pane", "-k", "-t", "%0", "stty -echo; printf 'READY\n'; exec cat")
		require.Eventually(t, func() bool {
			readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()
			update := options.Read(readCtx, status.Snapshot{})
			acceptStatusUpdate(update)
			for ref := range update.Values.All() {
				if ref.ID == before.ID && ref.Generation != before.Generation && update.Values.At(ref, time.Now()) == status.Idle {
					return update.Bindings.StateAt(windowItemID("$0", "@0"), update.Values, time.Now()) == status.Idle
				}
			}
			return false
		}, 5*time.Second, 20*time.Millisecond)
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
	second, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.Equal(t, first, second)
}

// pickerStatusSource serves a fixed view and signals a change on notify.
type pickerStatusSource struct {
	mu          sync.Mutex
	view        daemon.View
	unavailable bool
	changes     chan struct{}
}

func newPickerStatusSource(view daemon.View) *pickerStatusSource {
	return &pickerStatusSource{view: view, changes: make(chan struct{})}
}

func (s *pickerStatusSource) Changes() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes
}

func (s *pickerStatusSource) ReadView(context.Context) (daemon.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable {
		return daemon.View{}, daemon.ErrUnavailable
	}
	return s.view, nil
}

func (*pickerStatusSource) Close() error { return nil }

func (s *pickerStatusSource) setUnavailable(unavailable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unavailable = unavailable
	close(s.changes)
	s.changes = make(chan struct{})
}

func blockedPickerView(t *testing.T, identity string) daemon.View {
	t.Helper()
	ref := status.PaneRef{Server: identity, ID: "%0", Generation: "fixture"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		ref: {State: status.Blocked, ValidUntil: time.Now().Add(time.Minute)},
	})
	require.NoError(t, err)
	return daemon.View{
		Values: values, Locations: []daemon.Location{{Session: "$0", Window: "@0", Pane: "%0"}},
		Lease: time.Now().Add(time.Minute),
	}
}

func servePickerSource(t *testing.T, client *daemon.Client, identity string, source daemon.Source) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Serve(ctx, daemon.Binding{Server: identity, Alive: func(ctx context.Context) bool {
			return ctx.Err() == nil
		}}, func(context.Context, func(error)) (daemon.Source, error) { return source, nil })
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, awaitChannel(t, done))
	})
	require.Eventually(t, func() bool {
		_, err := client.Ping(ctx)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)
}

func TestNativePickerDeliversProtocolOutcomes(t *testing.T) {
	executable := buildPickerExecutable(t)
	b, client, run := pickerDaemonFixture(t)
	require.NoError(t, os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "hetki", "status.d", "cat.yaml")))
	runPickerPTY(t, executable, client)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	adapter.projection = projectionTree
	service := Service{Driver: adapter}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		// Only the readiness probe repeats; every transition below is sent once.
		ready := awaitPickerValue(t, ctx, options, status.Value{State: status.Idle}, func() {
			run("send-keys", "-t", "%0", "-l", "--", "\x1b]7501;state=idle\a\n")
		})
		require.Len(t, ready.Bindings, 2, "both linked occurrences must receive the report")

		for _, tc := range []struct {
			report string
			want   status.Value
		}{
			{"working", status.Value{State: status.Working}},
			{"done", status.Value{State: status.Unknown, Outcome: status.Completed}},
			{"error", status.Value{State: status.Unknown, Outcome: status.Failed}},
			{"working", status.Value{State: status.Working}},
			{"clear", status.Value{}},
		} {
			run("send-keys", "-t", "%0", "-l", "--", "\x1b]7501;state="+tc.report+"\a\n")
			update := awaitPickerValue(t, ctx, options, tc.want, nil)
			if tc.report == "done" {
				assertPickerSuccessAges(t, ctx, options, update, run)
			}
		}
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}

func assertPickerSuccessAges(t *testing.T, ctx context.Context, options ui.StatusOptions, first ui.StatusUpdate, run func(...string) string) {
	t.Helper()
	pane := firstPaneRef(t, first.Values)
	observed := first.Values.Lookup(pane).OutcomeSince
	require.False(t, observed.IsZero())
	nextReport := time.Now().Add(time.Second)

	idle := awaitPickerValue(t, ctx, options, status.Value{State: status.Idle}, func() {
		if time.Now().Before(nextReport) {
			return
		}
		run("send-keys", "-t", "%0", "-l", "--", "\x1b]7501;state=done\a\n")
		nextReport = time.Now().Add(time.Second)
	})

	require.True(t, observed.Equal(idle.Values.Lookup(pane).OutcomeSince), "repeated done must not restart the indication")
	require.GreaterOrEqual(t, time.Since(observed), 5*time.Second)
}

func awaitPickerValue(t *testing.T, ctx context.Context, options ui.StatusOptions, want status.Value, probe func()) ui.StatusUpdate {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	var lastGood ui.StatusUpdate
	var observed map[list.ItemID]status.Value
	for {
		if probe != nil {
			probe()
		}
		readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		update := options.Read(readCtx, status.Snapshot{})
		acceptStatusUpdate(update)
		cancel()
		if update.Err != nil {
			lastErr = update.Err
		} else {
			lastGood = update
			observed = pickerRowValues(update, time.Now())
			matched := len(observed) > 0
			for _, value := range observed {
				matched = matched && value == want
			}
			if matched {
				return update
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for picker value %+v: %v; last read error: %v; observed: %+v; bindings: %+v", want, ctx.Err(), lastErr, observed, lastGood.Bindings)
		case <-ticker.C:
		}
	}
}

func pickerRowValues(update ui.StatusUpdate, now time.Time) map[list.ItemID]status.Value {
	values := make(map[list.ItemID]status.Value, len(update.Bindings))
	for id := range update.Bindings {
		values[id] = update.Bindings.ValueAt(id, update.Values, now)
	}
	return values
}

func buildPickerExecutable(t *testing.T) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "hetki")
	build := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", executable, "../../..")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return executable
}

func pickerDaemonFixture(t testing.TB) (*tmux.TmuxBackend, *daemon.Client, func(...string) string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required")
	}
	dir := testenv.PrivateHome(t)
	socket := filepath.Join(dir, "tmux.sock")
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
	relative, err := filepath.Rel(dir, client.LogPath())
	require.NoError(t, err)
	require.True(t, filepath.IsLocal(relative), "daemon files must stay in the private test home")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Stop(ctx); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			t.Errorf("stop picker daemon: %v", err)
		}
		path := strings.TrimSuffix(client.LogPath(), ".log") + ".sock.lock"
		if err := testenv.WaitUnlocked(ctx, path); err != nil {
			t.Errorf("picker daemon teardown: %v", err)
		}
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
