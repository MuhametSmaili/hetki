package tui

import "github.com/MuhametSmaili/hetki/internal/terminal"

type confirmModalProps struct {
	LineWidth int
	Title     string
	Body      string
	Hint      string
	Theme     theme
}

func renderConfirmModal(props confirmModalProps) string {
	lines := []string{
		props.Theme.modalTitle.Render(terminal.Sanitize(props.Title)),
		terminal.Sanitize(props.Body),
		props.Theme.modalHint.Render(terminal.Sanitize(props.Hint)),
	}
	return renderBox(lines, props.LineWidth, 72, props.Theme.modal)
}
