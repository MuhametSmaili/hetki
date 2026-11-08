//go:build integration

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/app/actions"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

type actionFixture struct {
	bin         string
	socket      string
	source      string
	destination string
	recipes     string
}

func (f actionFixture) run(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, f.bin, append([]string{"-S", f.socket}, args...)...).CombinedOutput()
	require.NoError(t, err, "%s", output)
	return strings.TrimSpace(string(output))
}

func newActionFixture(t *testing.T) actionFixture {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	require.NoError(t, err)
	f := actionFixture{bin: bin, socket: filepath.Join(testenv.SocketDir(t), "actions.sock")}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f.source = f.run(t, "-f", "/dev/null", "new-session", "-d", "-x", "120", "-y", "30", "-s", "action-test", "-n", "source", "-P", "-F", "#{pane_id}", "/bin/sh")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		output, cleanupErr := exec.CommandContext(ctx, bin, "-S", f.socket, "kill-server").CombinedOutput()
		if cleanupErr != nil {
			t.Errorf("clean up action server: %v: %s", cleanupErr, output)
		}
	})
	f.destination = f.run(t, "new-window", "-d", "-t", "action-test:", "-n", "destination", "-P", "-F", "#{session_id}:#{window_id}", "/bin/sh")
	wrappers := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(wrappers, "tmux"), []byte(fmt.Sprintf("#!/bin/sh\nexec %q -S %q \"$@\"\n", bin, f.socket)), 0755))
	t.Setenv("PATH", wrappers+string(os.PathListSeparator)+os.Getenv("PATH"))
	f.recipes = filepath.Join(t.TempDir(), "actions.yaml")
	text := fmt.Sprintf(`version: 1
actions:
  join:
    label: Join source pane
    scope: [pane]
    steps:
      - select: destination
        title: Destination
        source: {builtin: tmux.windows}
      - run:
          argv: [%q, -S, "${context.server.socket}", join-pane, -d, -s, "${context.subject.pane_id}", -t, "${steps.destination.value.pane_target}"]
`, bin)
	require.NoError(t, os.WriteFile(f.recipes, []byte(text), 0600))
	return f
}

func TestActionFlowNativeJoinPane(t *testing.T) {
	f := newActionFixture(t)
	catalog, err := actions.Load(t.Context(), f.recipes)
	require.NoError(t, err)
	inventory, err := tmux.NewBackend()
	require.NoError(t, err)
	flow, err := actions.New(t.Context(), catalog, inventory, actions.Subject{Kind: "pane", PaneID: f.source})
	require.NoError(t, err)

	_, err = flow.Select(t.Context(), "join")
	require.NoError(t, err)
	screen, err := flow.Select(t.Context(), f.destination)

	require.NoError(t, err)
	require.True(t, screen.Done)
	panes := strings.Fields(f.run(t, "list-panes", "-t", f.destination, "-F", "#{pane_id}"))
	require.Len(t, panes, 2)
	require.Contains(t, panes, f.source)
}

func TestActionFlowNativeSubjectDirectory(t *testing.T) {
	f := newActionFixture(t)
	directory := filepath.Join(t.TempDir(), "repo with spaces")
	require.NoError(t, os.Mkdir(directory, 0700))
	directory, err := filepath.EvalSymlinks(directory)
	require.NoError(t, err)
	pane := f.run(t, "new-window", "-d", "-t", "action-test:", "-c", directory, "-P", "-F", "#{pane_id}", "/bin/sh")
	output := filepath.Join(t.TempDir(), "request.json")
	argument := filepath.Join(t.TempDir(), "argument.txt")
	catalog := actions.Catalog{Version: 1, Actions: map[string]actions.Recipe{
		"capture": {Label: "Capture directory", Scope: []string{"pane"}, Steps: []actions.Step{
			{Run: &actions.Command{Argv: []string{
				"sh", "-c", `cat > "$1"; printf '%s' "$2" > "$3"`, "capture", output, "${context.subject.cwd}", argument,
			}}},
		}},
	}}
	inventory, err := tmux.NewBackend()
	require.NoError(t, err)
	flow, err := actions.New(t.Context(), catalog, inventory, actions.Subject{Kind: "pane", PaneID: pane})
	require.NoError(t, err)

	screen, err := flow.Select(t.Context(), "capture")

	require.NoError(t, err)
	require.True(t, screen.Done)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	var request actions.Request
	require.NoError(t, json.Unmarshal(data, &request))
	require.Equal(t, directory, request.Context.Subject.CWD)
	data, err = os.ReadFile(argument)
	require.NoError(t, err)
	require.Equal(t, directory, string(data))
}

func TestEmbeddedActionFlowRefreshesNavigation(t *testing.T) {
	f := newActionFixture(t)
	data, err := os.ReadFile(f.recipes)
	require.NoError(t, err)
	configDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "hetki")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "actions.yaml"), data, 0600))
	b, err := tmux.NewBackend()
	require.NoError(t, err)
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	snapshot, err := adapter.Load(t.Context(), ui.ViewFlat)
	require.NoError(t, err)
	var subject list.ItemID
	for id := range list.IDs(snapshot.Items) {
		if id.ID == f.source {
			subject = id
		}
	}
	require.False(t, subject.IsZero())
	sessionID, windowID, ok := strings.Cut(f.destination, ":")
	require.True(t, ok)
	moved := destinationItemID(sessionID, windowID, f.source)
	require.NotContains(t, list.IDs(snapshot.Items), moved)

	menu, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionContextMenu, ItemID: subject})
	require.NoError(t, err)
	require.NotNil(t, menu.Menu)
	require.Contains(t, menu.Menu.Entries, ui.MenuEntry{Action: ui.ActionUserActions, Label: "User actions…"})
	result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionUserActions, ItemID: subject})
	require.NoError(t, err)
	require.NotNil(t, result.Choice)
	require.True(t, result.Choice.ResetHistory)
	require.NotNil(t, result.Snapshot)
	require.Len(t, result.Snapshot.Items, 1)
	result, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionUserActions, Choice: ui.ChoiceSelect, ItemID: result.Snapshot.Items[0].ID})
	require.NoError(t, err)
	require.NotNil(t, result.Choice)
	require.False(t, result.Choice.ResetHistory)
	require.NotNil(t, result.Snapshot)
	var destination list.ItemID
	for _, item := range result.Snapshot.Items {
		if item.ID.ID == f.destination {
			destination = item.ID
		}
	}
	require.False(t, destination.IsZero())
	result, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionUserActions, Choice: ui.ChoiceSelect, ItemID: destination})

	require.NoError(t, err)
	require.NotNil(t, result.Choice)
	require.True(t, result.Choice.Closed)
	require.NotNil(t, result.Snapshot)
	require.Contains(t, list.IDs(result.Snapshot.Items), moved)
	require.NotContains(t, list.IDs(result.Snapshot.Items), subject)
	panes := strings.Fields(f.run(t, "list-panes", "-t", f.destination, "-F", "#{pane_id}"))
	require.Len(t, panes, 2)
	require.Contains(t, panes, f.source)

	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionUserActions, Choice: ui.ChoiceSelect, ItemID: destination})

	require.ErrorContains(t, err, "action picker is not open")
}

func TestActionPickerPTY(t *testing.T) {
	if os.Getenv("HETKI_ACTION_PTY") == "1" {
		runActionPTYChild(t)
		return
	}
	for _, height := range []string{"100%", "12"} {
		t.Run(height, func(t *testing.T) {
			f := newActionFixture(t)
			runActionPTY(t, f, height)
			panes := strings.Fields(f.run(t, "list-panes", "-t", f.destination, "-F", "#{pane_id}"))
			require.Len(t, panes, 2)
			require.Contains(t, panes, f.source)
		})
	}
}

func runActionPTYChild(t *testing.T) {
	t.Helper()
	size := exec.Command("stty", "cols", "100", "rows", "24")
	size.Stdin = os.Stdin
	require.NoError(t, size.Run())
	before := ptyTerminalState(t)
	height, err := ui.ParseHeight(os.Getenv("HETKI_ACTION_HEIGHT"))
	require.NoError(t, err)
	err = OpenActions(t.Context(), actions.Subject{Kind: "pane", PaneID: os.Getenv("HETKI_ACTION_SUBJECT")}, os.Getenv("HETKI_ACTION_FILE"), Overrides{Height: &height})
	require.NoError(t, err)
	require.Equal(t, before, ptyTerminalState(t))
	fmt.Println("ACTION_DONE")
}

func runActionPTY(t *testing.T, f actionFixture, height string) {
	t.Helper()
	script, err := exec.LookPath("script")
	require.NoError(t, err)
	bin, err := os.Executable()
	require.NoError(t, err)
	args := []string{"-q", "/dev/null", bin, "-test.run=^TestActionPickerPTY$"}
	if runtime.GOOS != "darwin" {
		args = []string{"-q", "-e", "-c", fmt.Sprintf("%q -test.run=^TestActionPickerPTY$", bin), "/dev/null"}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, args...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "HETKI_ACTION_PTY=1", "TERM=xterm-256color", "HETKI_ACTION_SUBJECT="+f.source, "HETKI_ACTION_FILE="+f.recipes, "HETKI_ACTION_HEIGHT="+height)
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output := &ptyOutput{onWrite: ptyInput(input, []ptyStep{
		{marker: "Join source pane", keys: "\r"},
		{marker: "destination", keys: "destination\r"},
	})}
	cmd.Stdout, cmd.Stderr = output, output

	err = cmd.Run()

	require.NoError(t, errors.Join(err, output.err, ctx.Err()), "%s", output.buffer.String())
	require.Contains(t, output.buffer.String(), "ACTION_DONE")
}
