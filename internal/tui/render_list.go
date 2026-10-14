package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/terminal"
)

type rowProps struct {
	Primary    string
	Secondary  string
	Trailing   string
	Status     status.Value
	StatusTime time.Time
	JumpLabel  string
	Depth      int
	TreePrefix string
	Expanded   bool
	Branch     bool
	Active     bool
	Selected   bool
}

type listProps struct {
	Width     int
	EmptyText string
	Rows      []rowProps
	Compact   bool
	Theme     theme
}

func renderList(props listProps) []string {
	if len(props.Rows) == 0 {
		return []string{props.Theme.meta.Render(truncateWidth(props.EmptyText, props.Width))}
	}
	left := make([]string, len(props.Rows))
	columnWidth := 0
	secondaryWidth := 0
	trailingWidth := 0
	alignSecondary := false
	for i, row := range props.Rows {
		left[i] = renderRowLine(row, props.Theme, props.Compact)
		columnWidth = max(columnWidth, terminal.Width(left[i]))
		secondaryWidth = max(secondaryWidth, terminal.Width(strings.TrimSpace(terminal.Sanitize(row.Secondary))))
		trailingWidth = max(trailingWidth, rowTrailingWidth(row))
		alignSecondary = alignSecondary || (row.Depth == 0 && strings.TrimSpace(row.Secondary) != "")
	}
	const gap = 2
	const minSecondaryWidth = 6
	// Reserve the right column before fitting labels and paths. Long trailing
	// text must not consume the entire row, including in a narrow preview split.
	trailingWidth = min(trailingWidth, max(0, props.Width/3))
	bodyWidth := props.Width
	if trailingWidth > 0 {
		bodyWidth = max(0, props.Width-gap-trailingWidth)
	}
	if alignSecondary && secondaryWidth > 0 && bodyWidth >= gap+minSecondaryWidth {
		reserve := min(secondaryWidth, max(minSecondaryWidth, bodyWidth/2))
		columnWidth = min(columnWidth, bodyWidth-gap-reserve)
	}
	lines := make([]string, 0, len(props.Rows))
	for i, row := range props.Rows {
		line := composeRowLine(row, left[i], columnWidth, bodyWidth, props.Compact, alignSecondary, props.Theme)
		if trailingWidth > 0 {
			trailing := renderTrailing(row, trailingWidth, props.Theme)
			if trailing != "" {
				padding := max(0, props.Width-terminal.Width(line)-terminal.Width(trailing))
				// The path's nested style resets ANSI attributes. Reapply the
				// row style to both the remaining gap and right-hand text.
				line += props.Theme.itemStyle(row.Active, row.Selected).Render(strings.Repeat(" ", padding) + trailing)
			}
		}
		lines = append(lines, styleRowLine(row, line, props.Width, props.Theme))
	}
	return lines
}

func composeRowLine(row rowProps, left string, columnWidth, width int, compact, alignSecondary bool, styles theme) string {
	secondary := strings.TrimSpace(terminal.Sanitize(row.Secondary))
	if (compact && !alignSecondary) || secondary == "" {
		return truncateWidth(left, width)
	}
	const gap = 2
	const minSecondaryWidth = 6
	secondaryStyle := styles.secondary
	if row.Selected {
		secondaryStyle = styles.secondarySelected
	}
	leftWidth := terminal.Width(left)
	if !alignSecondary {
		available := width - leftWidth - gap
		if available < minSecondaryWidth {
			return truncateWidth(left, width)
		}
		shortened := shortenPath(secondary, available)
		padding := max(gap, width-leftWidth-terminal.Width(shortened))
		return left + strings.Repeat(" ", padding) + secondaryStyle.Render(shortened)
	}
	if width < gap+minSecondaryWidth {
		return truncateWidth(left, width)
	}
	leftBudget := min(columnWidth, width-gap-minSecondaryWidth)
	left = truncateWidth(left, leftBudget)
	leftWidth = terminal.Width(left)
	available := width - leftBudget - gap
	shortened := shortenPath(secondary, available)
	padding := leftBudget - leftWidth + gap
	return left + strings.Repeat(" ", padding) + secondaryStyle.Render(shortened)
}

func shortenPath(path string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	pathWidth := terminal.Width(path)
	if pathWidth <= maxWidth {
		return path
	}
	const ellipsis = "..."
	if maxWidth <= len(ellipsis) {
		return terminal.Truncate(path, maxWidth)
	}
	prefix, rest := "", path
	switch {
	case strings.HasPrefix(path, "~/"):
		prefix, rest = "~/", path[2:]
	case strings.HasPrefix(path, "/"):
		prefix, rest = "/", path[1:]
	}
	components := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' })
	if len(components) > 1 {
		tail := ""
		for i := len(components) - 1; i >= 0; i-- {
			candidateTail := components[i]
			if tail != "" {
				candidateTail += "/" + tail
			}
			candidate := prefix + ellipsis + "/" + candidateTail
			if terminal.Width(candidate) > maxWidth {
				break
			}
			tail = candidateTail
		}
		if tail != "" {
			return prefix + ellipsis + "/" + tail
		}
	}
	available := maxWidth - len(ellipsis)
	headWidth := available - available/2
	tailWidth := available / 2
	return terminal.Cut(path, 0, headWidth) + ellipsis + terminal.Cut(path, pathWidth-tailWidth, pathWidth)
}

func renderRowLine(row rowProps, styles theme, compact bool) string {
	indicator := " "
	if row.Active {
		indicator = "│"
	}
	parts := []string{indicator}
	if jumpLabel := displayJumpLabel(row.JumpLabel); jumpLabel != "" {
		parts = append(parts, jumpLabel)
	}
	return strings.Join(append(parts, decoratedLabel(row, compact)), " ")
}

func styleRowLine(row rowProps, line string, width int, styles theme) string {
	style := styles.itemStyle(row.Active, row.Selected)
	if row.Selected {
		return style.Width(width).Render(line)
	}
	rendered := style.Render(line)
	jumpLabel := displayJumpLabel(row.JumpLabel)
	if jumpLabel == "" || width <= 2 {
		return rendered
	}
	badge := terminal.Cut(styles.jumpLabel.Render(jumpLabel), 0, width-2)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(rendered),
		lipgloss.NewLayer(badge).X(2).Z(1),
	).Render()
}

func displayJumpLabel(label string) string {
	return strings.TrimSpace(terminal.Sanitize(label))
}

func decoratedLabel(row rowProps, compact bool) string {
	primary := strings.TrimSpace(terminal.Sanitize(row.Primary))
	if primary == "" {
		primary = "-"
	}
	if compact {
		return strings.Repeat("  ", max(row.Depth-1, 0)) + primary
	}
	prefix := row.TreePrefix
	branch := branchGlyph(row)
	if row.Depth == 0 {
		if !row.Branch {
			return primary
		}
		return branch + primary
	}
	return fmt.Sprintf("  %s%s", prefix, primary)
}

func branchGlyph(row rowProps) string {
	if !row.Branch {
		return "  "
	}
	if row.Expanded {
		return "\uf0d7 "
	}
	return "\uf0da "
}
