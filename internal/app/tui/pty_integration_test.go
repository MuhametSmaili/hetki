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
		b, err := backendtmux.NewBackend()
		require.NoError(t, err)
		driver := ptyDriver{navigate: func(ctx context.Context, target list.ItemID) error {
			require.Equal(t, sessionItemID("dev"), target)
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
		require.NoError(t, service.Run(context.Background()))
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
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "tmux"), []byte("#!/bin/sh\nprintf 'NAVIGATION\\n'\n"), 0755))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, args...)
	cmd.Env = append(os.Environ(), ptyHelperEnv+"=1", "TERM=xterm-256color", "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, inputWriter, err := os.Pipe()
	require.NoError(t, err)
	defer input.Close()
	cmd.Stdin = input
	go func() {
		time.Sleep(500 * time.Millisecond)
		_, _ = inputWriter.Write([]byte("\x1bp")) // Alt+P
		time.Sleep(500 * time.Millisecond)
		_, _ = inputWriter.Write([]byte("\r"))
		_ = inputWriter.Close()
	}()
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Run(), output.String())

	require.Contains(t, output.String(), "PREVIEW CONTENT")
	bytesOut := output.Bytes()
	restored := bytes.Index(bytesOut, []byte("\x1b[?1049l"))
	navigation := bytes.Index(bytesOut, []byte("NAVIGATION"))
	recorded := bytes.Index(bytesOut, []byte("RECORDED"))
	require.NotEqual(t, -1, restored, "alternate screen was not restored: %q", output.String())
	require.Greater(t, navigation, restored, "navigation ran before terminal restoration: %q", output.String())
	require.Greater(t, recorded, navigation, "recording ran before navigation succeeded: %q", output.String())
}
