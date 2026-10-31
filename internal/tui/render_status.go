package tui

import (
	"image/color"
	"strings"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

const (
	statusGap          = " "
	statusCellWidth    = 1
	commandStatusWidth = len(statusGap) + statusCellWidth
)

var statusSpinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func statusIndicator(value status.Value, now time.Time) string {
	switch value.State {
	case status.Working:
		frame := now.Nanosecond() / int(statusSpinnerPeriod)
		return statusSpinnerFrames[frame]
	case status.Blocked:
		return "?"
	case status.Idle:
		return "○"
	case status.Unknown:
		switch value.Outcome {
		case status.Completed:
			return "✓"
		case status.Failed:
			return "×"
		default:
			return "·"
		}
	default:
		return ""
	}
}

func rowTrailingWidth(row rowProps) int {
	width := terminal.Width(columnText(row.Trailing))
	glyph := statusIndicator(row.Status, row.StatusTime)
	if width == 0 && glyph == "" {
		return 0
	}
	return width + commandStatusWidth
}

func renderTrailing(row rowProps, width int, styles theme) string {
	if width <= 0 {
		return ""
	}

	style := styles.commandStyle(row.Active, row.Selected)
	commandWidth := max(0, width-commandStatusWidth)
	command := truncateWidth(columnText(row.Trailing), commandWidth)
	padding := strings.Repeat(" ", max(0, commandWidth-terminal.Width(command)))
	text := padding + command
	if width > statusCellWidth {
		text += statusGap
	}

	glyph := statusIndicator(row.Status, row.StatusTime)
	if glyph == "" {
		return style.Render(text + strings.Repeat(" ", statusCellWidth))
	}
	if row.Selected && style.GetReverse() {
		return style.Render(text + glyph)
	}

	indicator := style.Foreground(styles.palette.statusColor(row.Status))
	if row.Status.State == status.Unknown && row.Status.Outcome == status.NoOutcome {
		indicator = styles.meta.Inherit(style)
	}
	return style.Render(text) + indicator.Render(glyph)
}

func (p Palette) statusColor(value status.Value) color.Color {
	switch value.State {
	case status.Working:
		return p.Working
	case status.Blocked:
		return p.Blocked
	case status.Idle:
		return p.Idle
	default:
		switch value.Outcome {
		case status.Completed:
			return p.Success
		case status.Failed:
			return p.Error
		default:
			return p.Muted
		}
	}
}
