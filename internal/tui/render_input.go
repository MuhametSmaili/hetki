package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

type inputModalProps struct {
	LineWidth int
	MaxHeight int
	Title     string
	Prompt    string
	Value     string
	Hint      string
	Theme     theme
}

func renderInputModal(props inputModalProps) string {
	lines := []string{
		props.Theme.modalTitle.Render(terminal.Sanitize(props.Title)),
		terminal.Sanitize(props.Prompt),
		"> " + terminal.Sanitize(props.Value) + "_",
		props.Theme.modalHint.Render(terminal.Sanitize(props.Hint)),
	}
	style := props.Theme.modal
	if props.MaxHeight > 0 && props.MaxHeight < len(lines)+style.GetVerticalFrameSize() {
		style = lipgloss.NewStyle()
		switch props.MaxHeight {
		case 1:
			lines = lines[2:3]
		case 2:
			lines = lines[1:3]
		case 3:
			lines = lines[1:]
		}
	}
	return renderBox(lines, props.LineWidth, 72, style)
}
