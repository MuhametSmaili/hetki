package tui

import (
	"path/filepath"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/terminal"
)

func (m model) directorySuggestionLines(limit int) []string {
	if m.activeDirectoryField() == nil || m.busy || limit == 0 {
		return nil
	}
	d := m.directories
	paths := d.suggestions.Paths
	showMore := d.suggestions.Incomplete && limit > 1
	if showMore {
		limit--
	}
	start := max(0, d.cursor-limit+1)
	end := min(len(paths), start+limit)
	lines := make([]string, 0, end-start+1)
	for i := start; i < end; i++ {
		label := "  " + terminal.Sanitize(filepath.Base(filepath.Clean(paths[i]))) + "/"
		if i == d.cursor {
			label = m.theme.itemStyle(false, true).Render("> " + strings.TrimPrefix(label, "  "))
		}
		lines = append(lines, label)
	}
	if showMore {
		lines = append(lines, m.theme.modalHint.Render("More folders omitted; narrow or type the full path"))
	} else if d.suggestions.Incomplete {
		marker := m.theme.modalHint.Render("More omitted")
		if len(lines) == 0 {
			return []string{marker}
		}
		lines[0] = marker + " | " + lines[0]
	}
	return lines
}
