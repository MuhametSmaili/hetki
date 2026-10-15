package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
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
	width := terminal.Width(strings.TrimSpace(terminal.Sanitize(row.Trailing)))
	if glyph, _ := statusIndicator(row.Status, row.StatusTime); glyph != "" {
		if width > 0 {
			width++
		}
		width++
	}
	return width
}

func renderTrailing(row rowProps, width int, styles theme) string {
	command := strings.TrimSpace(terminal.Sanitize(row.Trailing))
	glyph, color := statusIndicator(row.Status, row.StatusTime)
	if glyph == "" {
		return truncateWidth(command, width)
	}
	style := styles.itemStyle(row.Active, row.Selected)
	indicator := style.Foreground(lipgloss.Color(color)).Render(glyph)
	if command == "" || width < 3 {
		return indicator
	}
	return indicator + style.Render(" "+truncateWidth(command, width-2))
}
