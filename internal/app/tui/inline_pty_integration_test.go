//go:build integration

package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

const inlinePTYEnv = "HETKI_INLINE_PTY_HELPER"

func TestInlineTerminalLifecycle(t *testing.T) {
	if mode := os.Getenv(inlinePTYEnv); mode != "" {
		runInlinePTYHelper(t, mode)
		return
	}
	for _, mode := range []string{"select", "cancel", "context", "resize", "error", "rows", "middle", "bottom", "configured", "palette", "themed", "monochrome"} {
		t.Run(mode, func(t *testing.T) {
			output := runInlinePTY(t, mode)

			require.Contains(t, output, "SHELL_BEFORE")
			require.Contains(t, output, "SHELL_AFTER")
			require.NotContains(t, output, "\x1b[?1049h")
			require.NotContains(t, output, "\x1b[2J", "must not clear the full main screen")
			require.NotContains(t, output, "\x1b[3J", "must not clear scrollback")

			switch mode {
			case "themed":
				require.Contains(t, output, "48;2;16;32;48")
			case "palette":
				require.Contains(t, output, "48;2;57;55;74", "default selection uses the soft lavender surface")
				require.Contains(t, output, "38;2;224;222;244", "selected session names use the selected text color")
			case "monochrome":
				requireMonochromeOutput(t, output)
			}

			if mode != "cancel" && mode != "context" && mode != "error" {
				require.Contains(t, output, "NAVIGATION_RESTORED")
			} else {
				require.NotContains(t, output, "NAVIGATION_RESTORED")
			}
		})
	}
}

func runInlinePTY(t *testing.T, mode string) string {
	t.Helper()
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script is required for PTY integration coverage")
	}
	testBinary, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	args := []string{"-q", "/dev/null", testBinary, "-test.run=^TestInlineTerminalLifecycle$"}
	if runtime.GOOS != "darwin" {
		args = []string{"-q", "-e", "-c", fmt.Sprintf("%q -test.run=^TestInlineTerminalLifecycle$", testBinary), "/dev/null"}
	}
	cmd := exec.CommandContext(ctx, script, args...)
	cmd.Env = append(os.Environ(), inlinePTYEnv+"="+mode, "TERM=xterm-256color")
	if mode == "palette" || mode == "themed" || mode == "monochrome" {
		noColor := ""
		if mode == "monochrome" {
			noColor = "anything"
		}
		cmd.Env = append(cmd.Env, "TMUX=", "COLORTERM=truecolor", "NO_COLOR="+noColor)
	}
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output := &inlinePTYOutput{onWrite: inlinePTYInput(input, mode)}
	cmd.Stdout, cmd.Stderr = output, output

	err = cmd.Run()

	require.NoError(t, err, "%s", output.buffer.String())
	return output.buffer.String()
}

func inlinePTYInput(input io.Writer, mode string) func([]byte) error {
	type step struct{ marker, keys string }
	steps := []step{{"dev", "\x1bp"}, {"PREVIEW CONTENT", "\r"}}
	switch mode {
	case "cancel":
		steps[1].keys = "\x03"
	case "resize":
		steps = []step{{"dev", "\x0b"}, {"RESIZE NOW", "\x12"}, {"RESIZED ACTION", "\r"}}
	case "error":
		steps = []step{{"dev", "\x0b"}, {"ACTION FAILED", "\x03"}}
	}
	stage := 0
	return func(all []byte) error {
		if stage == len(steps) || !bytes.Contains(all, []byte(steps[stage].marker)) {
			return nil
		}
		keys := steps[stage].keys
		stage++
		_, err := input.Write([]byte(keys))
		return err
	}
}

type inlinePTYOutput struct {
	buffer  bytes.Buffer
	onWrite func([]byte) error
}

func (o *inlinePTYOutput) Write(p []byte) (int, error) {
	n, err := o.buffer.Write(p)
	if err == nil {
		err = o.onWrite(o.buffer.Bytes())
	}
	return n, err
}

func runInlinePTYHelper(t *testing.T, mode string) {
	size := exec.Command("stty", "cols", "100", "rows", "24")
	size.Stdin = os.Stdin
	require.NoError(t, size.Run())
	before := inlineTerminalState(t)
	switch mode {
	case "middle":
		fmt.Print(strings.Repeat("\n", 11))
	case "bottom":
		fmt.Print(strings.Repeat("\n", 22))
	}
	fmt.Println("SHELL_BEFORE")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	driver := ptyDriver{navigate: func(_ context.Context, target list.ItemID) error {
		require.Equal(t, sessionItemID("dev"), target)
		require.Equal(t, before, inlineTerminalState(t), "terminal must be restored before navigation")
		fmt.Println("NAVIGATION_RESTORED")
		return nil
	}}
	options := ui.DefaultOptions()
	options.Height, options.Border = ui.Height{Value: 80, Percent: true}, ui.BorderNone
	if mode == "rows" {
		options.Height = ui.Height{Value: 12}
	}
	if mode == "configured" {
		privateConfig(t, "ui: {view: flat, scope: tracked, preview: true, preview_width: 70, height: '40%', border: rounded}")
		prepared, err := prepareService(ctx, nil, Overrides{
			Height:       new(options.Height),
			Border:       new(options.Border),
			Scope:        new(ui.ScopeAll),
			Preview:      new(false),
			View:         new(ui.ViewTree),
			PreviewWidth: new(45),
		})
		require.NoError(t, err)
		options = prepared.Options
		require.Equal(t, ui.ViewTree, options.Initial.View)
		require.Equal(t, ui.ScopeAll, options.Initial.Scope)
		require.False(t, options.Initial.Preview)
		require.Equal(t, 45, options.PreviewWidth)
		require.Equal(t, ui.Height{Value: 80, Percent: true}, options.Height)
		require.Equal(t, ui.BorderNone, options.Border)
	}
	if mode == "themed" || mode == "monochrome" {
		options = themedInlineOptions(t, ctx, options)
	}

	service := Service{
		Driver:  inlinePTYDriver{ptyDriver: driver, mode: mode},
		Options: options,
		RunUI: func(ctx context.Context, snapshot list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
			if mode == "context" {
				read := runtime.Preview
				runtime.Preview = func(ctx context.Context, request ui.PreviewRequest) (string, error) {
					cancel()
					return read(ctx, request)
				}
			}

			return ui.Run(ctx, snapshot, options, runtime)
		},
	}

	err := service.Run(ctx)

	if mode == "context" {
		require.ErrorIs(t, err, context.Canceled)
	} else {
		require.NoError(t, err)
	}
	require.Equal(t, before, inlineTerminalState(t))
	fmt.Println("SHELL_AFTER")
}

func themedInlineOptions(t *testing.T, ctx context.Context, options ui.Options) ui.Options {
	t.Helper()

	privateConfig(t, "theme: {text: '#123456', selection_fg: '#abcdef', selection_bg: '#102030'}")
	prepared, err := prepareService(ctx, nil, Overrides{Height: &options.Height, Border: &options.Border})
	require.NoError(t, err)

	return prepared.Options
}

func requireMonochromeOutput(t *testing.T, output string) {
	t.Helper()

	var parser ansi.Parser
	parser.SetParamsSize(32)
	var inverse bool
	parser.SetHandler(ansi.Handler{HandleCsi: func(cmd ansi.Cmd, params ansi.Params) {
		if cmd != 'm' {
			return
		}

		var style uv.Style
		uv.ReadStyle(params, &style)
		require.Nil(t, style.Fg, "monochrome output contains a foreground color")
		require.Nil(t, style.Bg, "monochrome output contains a background color")
		inverse = inverse || style.Attrs&uv.AttrReverse != 0
	}})

	sequences := regexp.MustCompile("\x1b\\[[0-9;:]*m").FindAllString(output, -1)
	for _, sequence := range sequences {
		for _, b := range []byte(sequence) {
			parser.Advance(b)
		}
	}

	require.True(t, inverse, "monochrome selection must remain visible")
}

type inlinePTYDriver struct {
	ptyDriver
	mode string
}

func (d inlinePTYDriver) Preview(ctx context.Context, request ui.PreviewRequest) (string, error) {
	text, err := d.ptyDriver.Preview(ctx, request)
	if d.mode == "themed" || d.mode == "monochrome" {
		text = "\x1b[31m" + text + "\x1b[0m"
	}
	return text, err
}

func (d inlinePTYDriver) Execute(ctx context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
	if request.ActionID == ui.ActionContextMenu {
		if d.mode == "error" {
			return ui.ActionResult{}, fmt.Errorf("ACTION FAILED")
		}
		return ui.ActionResult{Menu: &ui.ItemMenu{Title: "RESIZE TEST", Entries: []ui.MenuEntry{{Action: ui.ActionRefresh, Label: "RESIZE NOW"}}}}, nil
	}
	if request.ActionID != ui.ActionRefresh {
		return d.ptyDriver.Execute(ctx, request)
	}
	resize := exec.CommandContext(ctx, "stty", "cols", "60", "rows", "4")
	resize.Stdin = os.Stdin
	if err := resize.Run(); err != nil {
		return ui.ActionResult{}, err
	}
	return ui.ActionResult{Menu: &ui.ItemMenu{Title: "RESIZED MENU", Entries: []ui.MenuEntry{{Action: ui.ActionOpen, Label: "RESIZED ACTION"}}}}, nil
}

func inlineTerminalState(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("stty", "-a")
	cmd.Stdin = os.Stdin
	output, err := cmd.Output()
	require.NoError(t, err)
	// PENDIN is a transient kernel input-queue flag, not a terminal preference.
	fields := slices.DeleteFunc(strings.Fields(string(output)), func(field string) bool {
		flag := strings.TrimSuffix(field, ";")
		return flag == "pendin" || flag == "-pendin"
	})
	// Window dimensions deliberately change in the resize case.
	dimensions := regexp.MustCompile(`\b([0-9]+ (rows|columns)|(rows|columns) [0-9]+);?`)
	state := dimensions.ReplaceAllString(strings.Join(fields, " "), "")
	return strings.Join(strings.Fields(state), " ")
}
