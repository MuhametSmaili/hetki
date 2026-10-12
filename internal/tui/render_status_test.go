package tui

import (
	"fmt"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/terminal"
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
				frames[i], _ = statusIndicator(status.Value{State: test.state}, now.Add(time.Duration(i)*test.period))
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
		color   string
		outcome status.Outcome
	}{
		{"working without command", status.Working, "", 0, "⠋", colorAccent, status.NoOutcome},
		{"blocked without command", status.Blocked, "", 0, "?", colorBlocked, status.NoOutcome},
		{"idle without command", status.Idle, "", 0, "○", colorIdle, status.NoOutcome},
		{"unknown without command", status.Unknown, "", 0, "·", colorMuted, status.NoOutcome},
		{"working with command", status.Working, "opencode", 0, "⠋", colorAccent, status.NoOutcome},
		{"blocked later without command", status.Blocked, "", 750 * time.Millisecond, "?", colorBlocked, status.NoOutcome},
		{"blocked later with command", status.Blocked, "opencode", 750 * time.Millisecond, "?", colorBlocked, status.NoOutcome},
		{"success with command", status.Unknown, "pi", 0, "✓", colorSuccess, status.Completed},
		{"error with command", status.Unknown, "pi", 750 * time.Millisecond, "×", colorError, status.Failed},
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
				wantTrailing += " " + test.command
			}
			position := 40 - terminal.Width(wantTrailing)
			screen := uv.NewScreenBuffer(40, 1)

			line := renderList(listProps{Width: 40, Rows: []rowProps{row}, Theme: styles})[0]
			uv.NewStyledString(line).Draw(screen, screen.Bounds())

			require.Equal(t, 40, terminal.Width(line))
			require.Equal(t, wantTrailing, terminal.Sanitize(terminal.Cut(line, position, 40)))
			cell := screen.CellAt(position, 0)
			require.Equal(t, test.glyph, cell.Content)
			if test.glyph != " " {
				require.Equal(t, lipgloss.Color(test.color), cell.Style.Fg)
			}
			for x := range 40 {
				require.Equal(t, styles.selectedRow.GetBackground(), screen.CellAt(x, 0).Style.Bg)
			}
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
	for _, width := range []int{0, 1, 2, 3, 6, 12, 24, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			row := rowProps{
				Primary: "中👨‍👩‍👧éditor", Secondary: "~/src",
				Trailing: "\x1b]0;bad\aopencode", Status: status.Value{State: status.Working},
			}

			line := renderList(listProps{Width: width, Rows: []rowProps{row}})[0]

			require.LessOrEqual(t, terminal.Width(line), width)
			require.NotContains(t, line, "\x1b]0")
		})
	}
}
