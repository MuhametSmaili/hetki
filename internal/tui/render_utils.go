package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

func truncateWidth(s string, width int) string { return terminal.Truncate(s, width) }

func renderBox(lines []string, lineWidth, maxContentWidth int, style lipgloss.Style) string {
	frameWidth := style.GetHorizontalFrameSize()
	if lineWidth <= frameWidth {
		for i := range lines {
			lines[i] = truncateWidth(lines[i], lineWidth)
		}
		return strings.Join(lines, "\n")
	}
	boxWidth := min(maxContentWidth+frameWidth, lineWidth)
	contentWidth := boxWidth - frameWidth
	for i := range lines {
		lines[i] = truncateWidth(lines[i], contentWidth)
	}
	return style.Width(boxWidth).Render(strings.Join(lines, "\n"))
}
