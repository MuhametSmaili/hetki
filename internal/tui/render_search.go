package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/MSmaili/hetki/internal/terminal"
)

type searchBarProps struct {
	Width      int
	Filter     string
	Hint       string
	Right      string
	RightWidth int
	Scope      string
	Prompt     string
	Active     bool
	Theme      theme
}

func renderSearchBar(props searchBarProps) string {
	if props.Width <= 0 {
		return ""
	}
	search := strings.TrimSpace(terminal.Sanitize(props.Filter))
	text := ""
	if search != "" {
		text = " " + search
	}
	if props.Active {
		text += "█"
		if search == "" {
			text = " " + text
		}
	}

	right := renderHeaderMeta(props)
	rightW := terminal.Width(right)
	if rightW >= props.Width {
		return truncateWidth(right, props.Width)
	}
	leftW := props.Width
	if rightW > 0 {
		leftW -= rightW + 1
	}
	prompt := props.Theme.searchPrompt.Render(terminal.Sanitize(props.Prompt))
	left := terminal.Cut(prompt, 0, leftW)
	if promptW := terminal.Width(prompt); leftW > promptW {
		text = truncateWidth(text, leftW-promptW)
		left = prompt + props.Theme.searchBox.Render(text)
	}
	leftW = terminal.Width(left)
	rightX := props.Width - rightW
	hintText := strings.TrimSpace(terminal.Sanitize(props.Hint))
	hint := props.Theme.headerHint.Render(hintText)
	hintW := terminal.Width(hint)
	hintX := (props.Width - hintW) / 2
	if hintW > 0 && hintX > leftW && hintX+hintW < rightX {
		return left + strings.Repeat(" ", hintX-leftW) + hint +
			strings.Repeat(" ", rightX-hintX-hintW) + right
	}
	if rightW == 0 {
		return left
	}
	gap := props.Width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func renderHeaderMeta(props searchBarProps) string {
	var parts []string
	if props.Scope != "" {
		for _, scope := range []string{"All", "Tracked"} {
			style := props.Theme.meta
			if props.Scope == scope {
				style = props.Theme.searchBox.Underline(true)
			}
			parts = append(parts, style.Render(scope))
		}
	}
	if props.Right != "" {
		right := terminal.Sanitize(props.Right)
		width := max(props.RightWidth, terminal.Width(right))
		parts = append(parts, props.Theme.meta.Width(width).Align(lipgloss.Right).Render(right))
	}
	return strings.Join(parts, "  ")
}
