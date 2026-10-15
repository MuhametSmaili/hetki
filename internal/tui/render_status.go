package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

const (
	statusGap          = " "
	statusCellWidth    = 1
	commandStatusWidth = len(statusGap) + statusCellWidth
)

var statusSpinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func statusIndicator(value status.Value, now time.Time) (string, string) {
	switch value.State {
	case status.Working:
		frame := now.Nanosecond() / int(statusSpinnerPeriod)
		return statusSpinnerFrames[frame], colorAccent
	case status.Blocked:
		return "?", colorBlocked
	case status.Idle:
		return "○", colorIdle
	case status.Unknown:
		switch value.Outcome {
		case status.Completed:
			return "✓", colorSuccess
		case status.Failed:
			return "×", colorError
		default:
			return "·", colorMuted
		}
	default:
		return "", ""
	}
}

func rowTrailingWidth(row rowProps) int {
	width := terminal.Width(columnText(row.Trailing))
	glyph, _ := statusIndicator(row.Status, row.StatusTime)
	if width == 0 && glyph == "" {
		return 0
	}
	return width + commandStatusWidth
}

func renderTrailing(row rowProps, width int, styles theme) string {
	if width <= 0 {
		return ""
	}

	style := styles.itemStyle(row.Active, row.Selected)
	commandWidth := max(0, width-commandStatusWidth)
	command := truncateWidth(columnText(row.Trailing), commandWidth)
	padding := strings.Repeat(" ", max(0, commandWidth-terminal.Width(command)))
	text := padding + command
	if width > statusCellWidth {
		text += statusGap
	}

	glyph, color := statusIndicator(row.Status, row.StatusTime)
	if glyph == "" {
		return style.Render(text + strings.Repeat(" ", statusCellWidth))
	}
	return style.Render(text) + style.Foreground(lipgloss.Color(color)).Render(glyph)
}
