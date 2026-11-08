package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/directory"
)

type inputFieldState struct {
	InputField
	Value string
}

func newInputFields(fields []InputField) ([]inputFieldState, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(fields))
	states := make([]inputFieldState, 0, len(fields))
	for _, field := range fields {
		if field.ID == "" || strings.TrimSpace(field.Label) == "" || seen[field.ID] {
			return nil, fmt.Errorf("input fields require unique IDs and nonempty labels")
		}
		if field.Kind != InputText && field.Kind != InputDirectory {
			return nil, fmt.Errorf("unknown input kind %q", field.Kind)
		}
		if field.Kind == InputDirectory && !filepath.IsAbs(field.BasePath) {
			return nil, fmt.Errorf("directory field %q needs an absolute base path", field.ID)
		}
		seen[field.ID] = true
		states = append(states, inputFieldState{InputField: field, Value: field.InitialValue})
	}
	return states, nil
}

func (s *inputState) activeField() *inputFieldState {
	if s.Focus < 0 || s.Focus >= len(s.Fields) {
		return nil
	}
	return &s.Fields[s.Focus]
}

func (s *inputState) edit(edit func(string) string) {
	field := s.activeField()
	if field == nil {
		s.Value = edit(s.Value)
		return
	}
	field.Value = edit(field.Value)
}

func (m *model) appendInput(text string) {
	m.input.edit(func(value string) string { return value + text })
}

func (s *inputState) focusField(index int) {
	s.Focus = (index + len(s.Fields)) % len(s.Fields)
}

func (m model) formKey(msg tea.KeyPressMsg) (model, bool) {
	field := m.input.activeField()
	if field == nil {
		return m, false
	}
	switch msg.String() {
	case "shift+tab":
		m.input.focusField(m.input.Focus - 1)
	case "tab":
		if field.Kind != InputDirectory {
			m.input.focusField(m.input.Focus + 1)
		} else if paths := m.directories.suggestions.Paths; len(paths) > 0 {
			field.Value = paths[m.directories.cursor]
		}
	case "ctrl+l":
		if field.Kind != InputDirectory {
			return m, false
		}
		field.Value = ""
	case "up", "down", "ctrl+p", "ctrl+n":
		if field.Kind != InputDirectory {
			return m, false
		}
		direction := 1
		if msg.String() == "up" || msg.String() == "ctrl+p" {
			direction = -1
		}
		m.moveDirectorySuggestion(direction)
	default:
		return m, false
	}
	m.err = nil
	return m, true
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	values := make(map[string]string, len(m.input.Fields))
	for i, field := range m.input.Fields {
		value, err := field.submittedValue()
		if err != nil {
			m.err = err
			m.input.focusField(i)
			return m, nil
		}
		values[field.ID] = value
	}
	request := m.input.Request
	request.Values = values
	status := m.input.SubmitStatus
	if strings.TrimSpace(status) == "" {
		status = statusRunningAction
	}
	return m.startRequest(request, status)
}

func (f inputFieldState) submittedValue() (string, error) {
	empty := f.Value == ""
	if f.Kind == InputText {
		empty = strings.TrimSpace(f.Value) == ""
	}
	if empty && f.Required {
		return "", fmt.Errorf("%s is required", f.Label)
	}
	if f.Kind == InputDirectory && f.Value != "" {
		return directory.Resolve(f.Value, f.BasePath)
	}
	return f.Value, nil
}
