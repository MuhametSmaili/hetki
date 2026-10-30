package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

type menuProps struct {
	LineWidth int
	MaxHeight int
	Title     string
	Entries   []MenuEntry
	Selected  int
	Hint      string
	Keys      KeyMap
	Theme     theme
}

func renderMenu(props menuProps) string {
	style := props.Theme.modal
	entries, selected := props.Entries, props.Selected
	showTitle, showHint := true, true
	if props.MaxHeight > 0 {
		if props.MaxHeight <= style.GetVerticalFrameSize() {
			style = lipgloss.NewStyle()
		}
		contentRows := max(1, props.MaxHeight-style.GetVerticalFrameSize())
		showTitle = contentRows >= 2
		showHint = contentRows >= 3
		entryRows := contentRows
		if showTitle {
			entryRows--
		}
		if showHint {
			entryRows--
		}
		entries, selected = visibleMenuEntries(entries, selected, entryRows)
	}

	lines := make([]string, 0, len(entries)+2)
	if showTitle {
		lines = append(lines, props.Theme.modalTitle.Render(terminal.Sanitize(props.Title)))
	}
	for i, entry := range entries {
		key := ""
		if bound := props.Keys.Keys(KeyModeMenu, entry.Action); len(bound) > 0 {
			key = "‹" + terminal.Sanitize(strings.Join(bound, "/")) + "› "
		}
		label := terminal.Sanitize(entry.Label)
		if i == selected {
			lines = append(lines, props.Theme.itemStyle(false, true).Render(key+label))
			continue
		}
		lines = append(lines, props.Theme.menuKey.Render(key)+props.Theme.row.Render(label))
	}
	if showHint {
		lines = append(lines, props.Theme.modalHint.Render(terminal.Sanitize(props.Hint)))
	}
	return renderBox(lines, props.LineWidth, 48, style)
}

func visibleMenuEntries(entries []MenuEntry, selected, limit int) ([]MenuEntry, int) {
	limit = max(1, limit)
	if len(entries) <= limit {
		return entries, selected
	}
	selected = max(0, min(selected, len(entries)-1))
	start := min(max(0, selected-limit+1), len(entries)-limit)
	return entries[start : start+limit], selected - start
}
