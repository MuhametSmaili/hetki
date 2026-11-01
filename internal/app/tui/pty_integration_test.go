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
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

const ptyHelperEnv = "HETKI_TUI_PTY_HELPER"

type ptyDriver struct {
	navigate func(context.Context, list.ItemID) error
}

func (ptyDriver) Load(context.Context, ui.ViewMode) (list.Snapshot, error) {
	return list.Snapshot{Items: []list.Item{{
		ID: sessionItemID("dev"), Primary: "dev",
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "dev"}},
	}}}, nil
}

func (ptyDriver) Execute(_ context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
	if request.ActionID != ui.ActionOpen || request.ItemID != sessionItemID("dev") {
		return ui.ActionResult{}, fmt.Errorf("unexpected open request: %+v", request)
	}
	return ui.ActionResult{Navigation: request.ItemID}, nil
}

func (ptyDriver) Preview(context.Context, ui.PreviewRequest) (string, error) {
	return "PREVIEW CONTENT", nil
}

func (d ptyDriver) Navigate(ctx context.Context, target list.ItemID) error {
	return d.navigate(ctx, target)
}

func TestTerminalRestoredBeforeNavigation(t *testing.T) {
	if os.Getenv(ptyHelperEnv) == "1" {
		size := exec.Command("stty", "cols", "100", "rows", "24")
		size.Stdin = os.Stdin
		require.NoError(t, size.Run())
		before := ptyTerminalState(t)
		b, err := backendtmux.NewBackend()
		require.NoError(t, err)
		driver := ptyDriver{navigate: func(ctx context.Context, target list.ItemID) error {
			require.Equal(t, sessionItemID("dev"), target)
			require.Equal(t, before, ptyTerminalState(t), "terminal must be restored before navigation")
			if err := b.Switch(ctx, backend.Target{SessionID: "$1"}); err != nil {
				return err
			}
			fmt.Println("RECORDED")
			return nil
		}}
		service := Service{Options: ui.DefaultOptions(), Driver: driver,
			RunUI: func(ctx context.Context, snapshot list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
				result, err := ui.Run(ctx, snapshot, options, runtime)
				require.NoError(t, err)
				require.Equal(t, ui.Result{
					Navigation: sessionItemID("dev"),
					State:      ui.State{View: ui.ViewFlat, Scope: ui.ScopeAll, Preview: true},
				}, result)
				return result, err
			},
		}
		require.NoError(t, service.Run(t.Context()))
		return
	}

	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script is required for PTY integration coverage")
	}
	testBinary, err := os.Executable()
	require.NoError(t, err)

	var args []string
	if runtime.GOOS == "darwin" {
		args = []string{"-q", "/dev/null", testBinary, "-test.run=^TestTerminalRestoredBeforeNavigation$"}
	} else {
		command := fmt.Sprintf("%q -test.run=^TestTerminalRestoredBeforeNavigation$", testBinary)
		args = []string{"-q", "-e", "-c", command, "/dev/null"}
	}
	fakeBin := t.TempDir()
	command := "#!/bin/sh\n" +
		"[ \"$*\" = 'attach-session -t $1' ] || { printf 'unexpected tmux command: %s\\n' \"$*\" >&2; exit 1; }\n" +
		"printf 'NAVIGATION\\n'\n"
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "tmux"), []byte(command), 0755))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, args...)
	cmd.WaitDelay = time.Second
	// This fixture covers terminal handoff, not inside-tmux client attribution.
	cmd.Env = append(os.Environ(), ptyHelperEnv+"=1", "TERM=xterm-256color", "TMUX=", "TMUX_PANE=", "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output := &ptyOutput{onWrite: ptyInput(input, []ptyStep{
		{marker: "dev", keys: "\x1bp"},
		{marker: "PREVIEW CONTENT", keys: "\r"},
	})}
	cmd.Stdout, cmd.Stderr = output, output

	err = cmd.Run()

	require.NoError(t, errors.Join(err, output.err, ctx.Err()), "%s", output.buffer.String())
	require.Contains(t, output.buffer.String(), "PREVIEW CONTENT")
	bytesOut := output.buffer.Bytes()
	restored := bytes.Index(bytesOut, []byte("\x1b[?1049l"))
	navigation := bytes.Index(bytesOut, []byte("NAVIGATION"))
	recorded := bytes.Index(bytesOut, []byte("RECORDED"))
	require.NotEqual(t, -1, restored, "alternate screen was not restored: %q", output.buffer.String())
	require.Greater(t, navigation, restored, "navigation ran before terminal restoration: %q", output.buffer.String())
	require.Greater(t, recorded, navigation, "recording ran before navigation succeeded: %q", output.buffer.String())
}
