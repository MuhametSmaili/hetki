package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

type searchBarProps struct {
	Width  int
	Filter string
	Hint   string
	Meta   string // Styled metadata from renderHeaderMeta.
	Prompt string
	Active bool
	Theme  theme
}

type headerMetaProps struct {
	Scope      string
	Right      string
	RightWidth int
	Theme      theme
}

func renderSearchBar(props searchBarProps) string {
	if props.Width <= 0 {
		return ""
	}
	content := strings.TrimSpace(terminal.Sanitize(props.Filter))
	textStyle := props.Theme.searchBox
	if !props.Active && content == "" {
		content = strings.TrimSpace(terminal.Sanitize(props.Hint))
		textStyle = props.Theme.headerHint
	}
	text := ""
	if content != "" {
		text = " " + content
	}
	if props.Active {
		text += "█"
		if content == "" {
			text = " " + text
		}
	}

	right := props.Meta
	rightW := terminal.Width(right)
	if rightW >= props.Width {
		return truncateWidth(right, props.Width)
	}
	leftW := props.Width
	if rightW > 0 {
		leftW -= rightW + 1
	}
	prompt := props.Theme.filterPrompt.Render(terminal.Sanitize(props.Prompt))
	left := terminal.Cut(prompt, 0, leftW)
	if promptW := terminal.Width(prompt); leftW > promptW {
		text = truncateWidth(text, leftW-promptW)
		left = prompt + textStyle.Render(text)
	}
	leftW = terminal.Width(left)
	if rightW == 0 {
		return left
	}
	gap := props.Width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func renderHeaderRule(width int, label string, styles theme) string {
	if width <= 0 {
		return ""
	}
	label = strings.TrimSpace(truncateWidth(terminal.Sanitize(label), max(0, width-2)))
	if label == "" {
		return styles.sectionLine.Render(strings.Repeat("─", width))
	}

	labelWidth := terminal.Width(label)
	return styles.meta.Render(label) + " " + styles.sectionLine.Render(strings.Repeat("─", width-labelWidth-1))
}

func renderHeaderMeta(props headerMetaProps) string {
	var parts []string
	if props.Scope != "" {
		for _, scope := range []string{"All", "Tracked"} {
			style := props.Theme.meta
			if props.Scope == scope {
				style = props.Theme.scopeActive
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
