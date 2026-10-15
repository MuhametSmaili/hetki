package tui

import "github.com/MuhametSmaili/hetki/internal/terminal"

type inputModalProps struct {
	LineWidth int
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
	return renderBox(lines, props.LineWidth, 72, props.Theme.modal)
}
