package tui

import (
	"fmt"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestStatusIndicatorFrames(t *testing.T) {
	for _, test := range []struct {
		name   string
		state  status.State
		period time.Duration
		want   []string
	}{
		{"spinner wraps after ten frames", status.Working, 100 * time.Millisecond, []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏", "⠋"}},
		{"blocked stays visible", status.Blocked, 750 * time.Millisecond, []string{"?", "?", "?"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			frames := make([]string, len(test.want))

			for i := range frames {
				frames[i] = statusIndicator(status.Value{State: test.state}, now.Add(time.Duration(i)*test.period))
			}

			require.Equal(t, test.want, frames)
		})
	}
}

func TestStatusRenderingPreservesSelectionAndTrailingContent(t *testing.T) {
	for _, test := range []struct {
		name    string
		state   status.State
		command string
		offset  time.Duration
		glyph   string
		outcome status.Outcome
	}{
		{"working without command", status.Working, "", 0, "⠋", status.NoOutcome},
		{"blocked without command", status.Blocked, "", 0, "?", status.NoOutcome},
		{"idle without command", status.Idle, "", 0, "○", status.NoOutcome},
		{"unknown without command", status.Unknown, "", 0, "·", status.NoOutcome},
		{"working with command", status.Working, "opencode", 0, "⠋", status.NoOutcome},
		{"blocked later without command", status.Blocked, "", 750 * time.Millisecond, "?", status.NoOutcome},
		{"blocked later with command", status.Blocked, "opencode", 750 * time.Millisecond, "?", status.NoOutcome},
		{"success with command", status.Unknown, "pi", 0, "✓", status.Completed},
		{"error with command", status.Unknown, "pi", 0, "×", status.Failed},
	} {
		t.Run(test.name, func(t *testing.T) {
			stamp := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).Add(test.offset)
			styles := defaultTheme()
			row := rowProps{
				Primary: "editor", Depth: 1, TreePrefix: "└─ ", Selected: true,
				Status: status.Value{State: test.state, Outcome: test.outcome}, StatusTime: stamp, Trailing: test.command,
			}
			wantTrailing := test.glyph
			if test.command != "" {
				wantTrailing = test.command + " " + test.glyph
			}
			position := 40 - terminal.Width(wantTrailing)
			screen := lipgloss.NewCanvas(40, 1)

			line := renderList(listProps{Width: 40, Rows: []rowProps{row}, Theme: styles})[0]
			uv.NewStyledString(line).Draw(screen, screen.Bounds())

			require.Equal(t, 40, terminal.Width(line))
			require.Equal(t, wantTrailing, terminal.Sanitize(terminal.Cut(line, position, 40)))
			cell := screen.CellAt(39, 0)
			require.Equal(t, test.glyph, cell.Content)
			requireSelectedStyle(t, line, 40, styles)
		})
	}
}

func TestAbsentStatusRendersWithoutIndicator(t *testing.T) {
	for _, test := range []struct {
		name  string
		state status.State
	}{
		{"unreported", status.Unreported},
		{"withdrawn", status.Withdrawn},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := rowProps{Primary: "editor", Status: status.Value{State: test.state}}

			line := renderList(listProps{Width: 40, Rows: []rowProps{row}})[0]

			require.Equal(t, "  editor", terminal.Sanitize(line))
		})
	}
}

func TestStatusRowsFitNarrowWidths(t *testing.T) {
	for width := range 81 {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			row := rowProps{
				Primary: "中👨‍👩‍👧éditor", Secondary: "~/src", Depth: 1, TreePrefix: "└─ ",
				Selected: true, Active: true,
				Trailing: "\x1b]0;bad\aopencode", Status: status.Value{State: status.Working},
			}

			styles := defaultTheme()
			line := renderList(listProps{Width: width, Rows: []rowProps{row}, Theme: styles})[0]

			require.Equal(t, width, terminal.Width(line))
			require.NotContains(t, line, "\x1b]0")
			requireSelectedStyle(t, line, width, styles)
		})
	}
}
