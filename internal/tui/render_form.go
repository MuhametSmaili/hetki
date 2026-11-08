package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/directory"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

func (m model) viewForm(width, height int) string {
	style := m.theme.modal
	if height < 6 {
		style = lipgloss.NewStyle()
	}
	rows := max(1, height-style.GetVerticalFrameSize())
	fields := m.formFields(max(1, min(72, width-style.GetHorizontalFrameSize())))
	feedback := m.formFeedback()
	if rows < len(fields)+2 {
		return renderBox(compactForm(fields, feedback, m.input.Focus, rows), width, 72, style)
	}
	hints := m.formHints(rows)
	reserved := len(fields) + len(hints)
	if feedback != "" {
		reserved++
	}
	lines := make([]string, 0, rows)
	if rows > reserved {
		lines = append(lines, m.theme.modalTitle.Render(terminal.Sanitize(m.input.Title)))
	}
	lines = append(lines, fields...)
	if feedback != "" {
		lines = append(lines, m.theme.modalHint.Render(terminal.Sanitize(feedback)))
	}
	lines = append(lines, m.directorySuggestionLines(max(0, rows-len(lines)-len(hints)))...)
	lines = append(lines, hints...)
	return renderBox(lines, width, 72, style)
}

func (m model) formHints(rows int) []string {
	label := m.input.SubmitLabel
	if label == "" {
		label = "submit"
	}
	controls := modalControls(m.keys, KeyModePrompt, label)
	navigation := "tab next | shift+tab previous"
	if m.activeDirectoryField() != nil {
		navigation = "↑/ctrl+p ↓/ctrl+n choose | tab complete | shift+tab previous"
	}
	if rows >= len(m.input.Fields)+4 {
		return []string{m.theme.modalHint.Render(navigation), m.theme.modalHint.Render(controls)}
	}
	return []string{m.theme.modalHint.Render(controls + " | " + navigation)}
}

func (m model) formFields(width int) []string {
	labelWidth := 0
	for _, field := range m.input.Fields {
		labelWidth = max(labelWidth, terminal.Width(terminal.Sanitize(field.Label)))
	}
	lines := make([]string, 0, len(m.input.Fields))
	for i, field := range m.input.Fields {
		label := terminal.Sanitize(field.Label)
		label += strings.Repeat(" ", labelWidth-terminal.Width(label)+2)
		lines = append(lines, label+renderInputField(field, i == m.input.Focus, max(1, width-terminal.Width(label)), m.theme))
	}
	return lines
}

func renderInputField(field inputFieldState, active bool, width int, styles theme) string {
	value := terminal.Sanitize(field.Value)
	if value == "" {
		placeholder := styles.headerHint.Render(truncateWidth(terminal.Sanitize(field.Placeholder), max(0, width-2)))
		if active {
			return "_ " + placeholder
		}
		return placeholder
	}
	if active {
		value += "_"
	}
	if terminal.Width(value) > width {
		value = "…" + terminal.Cut(value, terminal.Width(value)-max(0, width-1), terminal.Width(value))
	}
	if active {
		return styles.searchBox.Render(value)
	}
	return value
}

func (m model) formFeedback() string {
	if m.err != nil {
		return m.err.Error()
	}
	if m.busy {
		return m.status
	}
	field := m.activeDirectoryField()
	if field == nil {
		return ""
	}
	if m.directories.err != nil {
		return fmt.Sprintf("Suggestions unavailable: %v; you can still type a path", m.directories.err)
	}
	path, err := directory.Resolve(field.Value, field.BasePath)
	if err != nil {
		return err.Error()
	}
	return "Resolved: " + path
}

func compactForm(fields []string, feedback string, focus, rows int) []string {
	if rows > len(fields) {
		return append(fields, terminal.Sanitize(feedback))
	}
	if feedback != "" {
		if rows == 1 {
			return []string{terminal.Sanitize(feedback)}
		}
		return []string{fields[focus], terminal.Sanitize(feedback)}
	}
	start := min(max(0, focus-rows+1), len(fields)-rows)
	return fields[start : start+rows]
}
