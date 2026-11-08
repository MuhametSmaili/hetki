package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/directory"
)

// DirectorySuggestFunc reads directory matches.
// The caller must cancel and wait for active calls.
type DirectorySuggestFunc func(context.Context, string, string) (directory.Suggestions, error)

type directoryState struct {
	fieldID     string
	value       string
	base        string
	generation  uint64
	cancel      context.CancelFunc
	ready       bool
	suggestions directory.Suggestions
	cursor      int
	err         error
}

type directoryResultMsg struct {
	generation  uint64
	suggestions directory.Suggestions
	err         error
}

func deleteLastPathComponent(value string) string {
	value = strings.TrimRight(value, "/")
	return value[:strings.LastIndexByte(value, '/')+1]
}

func (m *model) moveDirectorySuggestion(direction int) {
	m.directories.cursor = max(0, min(len(m.directories.suggestions.Paths)-1, m.directories.cursor+direction))
}

func (m *model) activeDirectoryField() *inputFieldState {
	field := m.input.activeField()
	if m.mode != modeInput || field == nil || field.Kind != InputDirectory {
		return nil
	}
	return field
}

// syncDirectorySuggestions keeps only one directory search active.
func (m model) syncDirectorySuggestions() (model, tea.Cmd) {
	d := &m.directories
	fieldID, value, base := "", "", ""
	if field := m.activeDirectoryField(); field != nil && !m.quitting && !m.busy {
		fieldID, value, base = field.ID, field.Value, field.BasePath
	}
	if fieldID != d.fieldID || value != d.value || base != d.base {
		d.fieldID, d.value, d.base = fieldID, value, base
		d.generation++
		d.ready, d.err, d.suggestions, d.cursor = false, nil, directory.Suggestions{}, 0
		if d.cancel != nil {
			d.cancel()
		}
	}
	if base == "" || d.ready || d.cancel != nil || m.readDirectories == nil {
		return m, nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 250*time.Millisecond)
	d.cancel = cancel
	generation, read := d.generation, m.readDirectories
	return m, func() tea.Msg {
		suggestions, err := read(ctx, value, base)
		return directoryResultMsg{generation: generation, suggestions: suggestions, err: err}
	}
}

func (m model) handleDirectoryResult(msg directoryResultMsg) (tea.Model, tea.Cmd) {
	d := &m.directories
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	if msg.generation == d.generation {
		d.ready, d.err, d.suggestions = true, msg.err, msg.suggestions
	}
	return m, nil
}
