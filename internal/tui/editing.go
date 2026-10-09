package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/charmbracelet/x/ansi"
)

func handleBackspace(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	return m.editValue(ActionBackspace, func(value string) string {
		runes := []rune(value)
		if len(runes) == 0 {
			return value
		}
		return string(runes[:len(runes)-1])
	})
}

func handleDeleteWord(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	return m.editValue(ActionDeleteWord, deleteLastWord)
}

func handleDeleteToStart(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	return m.editValue(ActionDeleteToStart, func(string) string { return "" })
}

func (m model) editValue(action ActionID, edit func(string) string) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		m.setQuery(edit(m.items.Query()))
		return m.reflow(), nil
	case modeInput:
		m.input.Value = edit(m.input.Value)
		m.err = nil
		return m, nil
	default:
		return m.actionUnavailable(action)
	}
}

func singleLinePaste(content string) string {
	content = ansi.Strip(strings.TrimRight(content, "\r\n"))
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			return ' '
		case r == utf8.RuneError || unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, content)
}

func deleteLastWord(value string) string {
	runes := []rune(value)
	end := len(runes)
	for end > 0 && (runes[end-1] == ' ' || runes[end-1] == '\t') {
		end--
	}
	for end > 0 && runes[end-1] != ' ' && runes[end-1] != '\t' {
		end--
	}
	return string(runes[:end])
}
