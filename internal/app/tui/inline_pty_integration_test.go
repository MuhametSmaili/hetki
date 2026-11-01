//go:build integration

package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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
				foreground := color.RGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff}
				background := color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}
				requirePTYSelectionColors(t, output, foreground, background)
			case "palette":
				palette := ui.DefaultPalette()
				requirePTYSelectionColors(t, output, palette.SelectionFG, palette.SelectionBG)
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
	cmd.WaitDelay = time.Second
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
	output := &ptyOutput{onWrite: inlinePTYInput(input, mode)}
	cmd.Stdout, cmd.Stderr = output, output

	err = cmd.Run()

	require.NoError(t, errors.Join(err, output.err, ctx.Err()), "%s", output.buffer.String())
	return output.buffer.String()
}

func inlinePTYInput(input io.Writer, mode string) func([]byte) error {
	steps := []ptyStep{
		{marker: "dev", keys: "\x1bp"},
		{marker: "PREVIEW CONTENT", keys: "\r"},
	}
	switch mode {
	case "cancel":
		steps[1].keys = "\x03"
	case "resize":
		steps = []ptyStep{
			{marker: "dev", keys: "\x0b"},
			{marker: "RESIZE NOW", keys: "\x12"},
			{marker: "RESIZED ACTION", keys: "\r"},
		}
	case "error":
		steps = []ptyStep{
			{marker: "dev", keys: "\x0b"},
			{marker: "ACTION FAILED", keys: "\x03"},
		}
	}
	return ptyInput(input, steps)
}

func runInlinePTYHelper(t *testing.T, mode string) {
	t.Helper()
	size := exec.Command("stty", "cols", "100", "rows", "24")
	size.Stdin = os.Stdin
	require.NoError(t, size.Run())
	before := ptyTerminalState(t)
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
		require.Equal(t, before, ptyTerminalState(t), "terminal must be restored before navigation")
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
	require.Equal(t, before, ptyTerminalState(t))
	fmt.Println("SHELL_AFTER")
}

func themedInlineOptions(t *testing.T, ctx context.Context, options ui.Options) ui.Options {
	t.Helper()

	privateConfig(t, "theme: {text: '#123456', selection_fg: '#abcdef', selection_bg: '#102030'}")
	prepared, err := prepareService(ctx, nil, Overrides{Height: &options.Height, Border: &options.Border})
	require.NoError(t, err)

	return prepared.Options
}

func requirePTYSelectionColors(t *testing.T, output string, foreground, background color.Color) {
	t.Helper()
	wantForeground := color.RGBAModel.Convert(foreground)
	wantBackground := color.RGBAModel.Convert(background)
	var parser ansi.Parser
	parser.SetParamsSize(32)
	var pen uv.Style
	var selectedText strings.Builder
	var found bool
	parser.SetHandler(ansi.Handler{
		HandleCsi: func(cmd ansi.Cmd, params ansi.Params) {
			if cmd == 'm' {
				uv.ReadStyle(params, &pen)
			}
		},
		Print: func(r rune) {
			if found {
				return
			}
			if pen.Fg == nil || pen.Bg == nil ||
				color.RGBAModel.Convert(pen.Fg) != wantForeground ||
				color.RGBAModel.Convert(pen.Bg) != wantBackground {
				selectedText.Reset()
				return
			}
			selectedText.WriteRune(r)
			found = strings.Contains(selectedText.String(), "dev")
		},
	})
	for _, b := range []byte(output) {
		parser.Advance(b)
	}

	require.True(t, found, "selected session name must use foreground %v and background %v", wantForeground, wantBackground)
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
