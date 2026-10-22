package tui

import (
	"strings"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

type rowProps struct {
	Context    string
	Annotation string
	Primary    string
	Secondary  string
	Trailing   string
	Status     status.Value
	StatusTime time.Time
	Depth      int
	TreePrefix string
	Expanded   bool
	Branch     bool
	Active     bool
	Selected   bool
}

type listProps struct {
	Width     int
	Height    int // Zero means unconstrained; one forces single-line items.
	EmptyText string
	Rows      []rowProps
	Compact   bool
	Theme     theme
}

func renderList(props listProps) []string {
	if len(props.Rows) == 0 {
		return []string{props.Theme.meta.Render(truncateWidth(props.EmptyText, props.Width))}
	}
	if props.Rows[0].Context != "" {
		return renderColumnList(props)
	}

	left := make([]string, len(props.Rows))
	columnWidth := 0
	secondaryWidth := 0
	trailingWidth := commandStatusWidth
	for i, row := range props.Rows {
		left[i] = renderRowLine(row, props.Compact)
		columnWidth = max(columnWidth, terminal.Width(left[i]))
		secondaryWidth = max(secondaryWidth, terminal.Width(strings.TrimSpace(terminal.Sanitize(row.Secondary))))
		trailingWidth = max(trailingWidth, rowTrailingWidth(row))
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
	if secondaryWidth > 0 && bodyWidth >= gap+minSecondaryWidth {
		reserve := min(secondaryWidth, max(minSecondaryWidth, bodyWidth/2))
		columnWidth = min(columnWidth, bodyWidth-gap-reserve)
	}
	lines := make([]string, 0, len(props.Rows))
	for i, row := range props.Rows {
		line := composeRowLine(row, left[i], columnWidth, bodyWidth, props.Theme)
		if trailingWidth > 0 && rowTrailingWidth(row) > 0 {
			trailing := renderTrailing(row, trailingWidth, props.Theme)
			padding := max(0, props.Width-terminal.Width(line)-trailingWidth)
			// The path resets ANSI attributes; style the gap to retain selection.
			line += props.Theme.itemStyle(row.Active, row.Selected).Render(strings.Repeat(" ", padding)) + trailing
		}
		lines = append(lines, styleRowLine(row, line, props.Width, props.Theme))
	}
	return lines
}

func composeRowLine(row rowProps, left string, columnWidth, width int, styles theme) string {
	secondary := strings.TrimSpace(terminal.Sanitize(row.Secondary))
	if secondary == "" {
		return truncateWidth(left, width)
	}
	const gap = 2
	const minSecondaryWidth = 6
	secondaryStyle := styles.secondary
	if row.Selected {
		secondaryStyle = styles.secondarySelected
	}
	if width < gap+minSecondaryWidth {
		return truncateWidth(left, width)
	}
	leftBudget := min(columnWidth, width-gap-minSecondaryWidth)
	left = truncateWidth(left, leftBudget)
	leftWidth := terminal.Width(left)
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

func renderRowLine(row rowProps, compact bool) string {
	return renderRowPrefix(row) + decoratedLabel(row, compact)
}

func renderRowPrefix(row rowProps) string {
	indicator := " "
	if row.Active {
		indicator = "│"
	}
	return indicator + " "
}

func styleRowLine(row rowProps, line string, width int, styles theme) string {
	style := styles.itemStyle(row.Active, row.Selected)
	if row.Selected {
		return style.Width(width).Render(line)
	}
	return style.Render(line)
}

func decoratedLabel(row rowProps, compact bool) string {
	primary := strings.TrimSpace(terminal.Sanitize(row.Primary))
	if primary == "" {
		primary = "-"
	}
	branch := branchGlyph(row)
	if row.Depth == 0 {
		if !row.Branch {
			return primary
		}
		return branch + primary
	}
	if compact {
		return row.TreePrefix + primary
	}
	return "  " + row.TreePrefix + primary
}

func branchGlyph(row rowProps) string {
	if !row.Branch {
		return "  "
	}
	if row.Expanded {
		return "▾ "
	}
	return "▸ "
}
