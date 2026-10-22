package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MuhametSmaili/hetki/internal/terminal"
)

const (
	columnGap          = "  "
	contextSeparator   = " › "
	minColumnPathWidth = 6
)

// Below this list width a flat row trades its columns for two stacked lines:
// identity first, then path and trailing. The threshold matches compact tree
// rows. A one-line viewport falls back to columns rather than overflowing.
const narrowListWidth = 56

type columnWidths struct {
	prefix     int
	context    int
	primary    int
	annotation int
	path       int
	trailing   int
}

func renderColumnList(props listProps) []string {
	if props.Width < narrowListWidth && props.Height != 1 {
		return renderStackedList(props)
	}

	columns := measureColumns(props.Rows).fit(props.Width)
	lines := make([]string, len(props.Rows))
	for i, row := range props.Rows {
		lines[i] = renderColumnRow(row, columns, props.Width, props.Theme)
	}
	return lines
}

// renderStackedList emits exactly two lines per row so the caller can halve
// its visible row budget and page by items instead of lines.
func renderStackedList(props listProps) []string {
	trailingWidth := 0
	for _, row := range props.Rows {
		trailingWidth = max(trailingWidth, rowTrailingWidth(row))
	}

	trailingWidth = min(trailingWidth, props.Width, max(1, props.Width/3))

	lines := make([]string, 0, 2*len(props.Rows))
	for _, row := range props.Rows {
		lines = append(lines,
			styleRowLine(row, stackedIdentity(row, props.Width, props.Theme), props.Width, props.Theme),
			stackedDetailLine(row, trailingWidth, props.Width, props.Theme),
		)
	}
	return lines
}

func stackedIdentity(row rowProps, width int, styles theme) string {
	prefix := terminal.Cut(renderRowPrefix(row), 0, width)
	style := styles.itemStyle(row.Active, row.Selected)
	muted := styles.meta.Inherit(style)
	label := columnText(row.Context) + muted.Render(contextSeparator) + style.Render(columnText(row.Primary))
	if annotation := columnText(row.Annotation); annotation != "" {
		label += muted.Render(columnGap + annotation)
	}

	budget := max(0, width-terminal.Width(prefix))
	return prefix + truncateWidth(label, budget)
}

func stackedDetailLine(row rowProps, trailingWidth, width int, styles theme) string {
	detail := row
	detail.JumpLabel = ""
	style := styles.itemStyle(detail.Active, detail.Selected)
	pathStyle := styles.secondary.Inherit(style)
	if detail.Selected {
		pathStyle = styles.secondarySelected.Inherit(style)
	}

	if rowTrailingWidth(detail) == 0 {
		trailingWidth = 0
	}
	bodyWidth := width
	if trailingWidth > 0 {
		bodyWidth = max(0, width-trailingWidth-len(columnGap))
	}
	indent := terminal.Cut(stackedIndent(row), 0, bodyWidth)
	budget := max(0, bodyWidth-terminal.Width(indent))
	line := indent + pathStyle.Render(shortenPath(columnText(detail.Secondary), budget))
	if trailingWidth > 0 {
		padding := max(0, width-terminal.Width(line)-trailingWidth)
		line += style.Render(strings.Repeat(" ", padding)) + renderTrailing(detail, trailingWidth, styles)
	}
	return styleRowLine(detail, truncateWidth(line, width), width, styles)
}

// stackedIndent keeps the detail line under the label: it reserves the
// jump-label gutter and repeats the active indicator without the label.
func stackedIndent(row rowProps) string {
	indicator := " "
	if row.Active {
		indicator = "│"
	}
	label := displayJumpLabel(row.JumpLabel)
	if label == "" {
		return indicator + " "
	}
	return indicator + " " + strings.Repeat(" ", terminal.Width(label)) + " "
}

func measureColumns(rows []rowProps) columnWidths {
	var columns columnWidths
	for _, row := range rows {
		columns.prefix = max(columns.prefix, terminal.Width(renderRowPrefix(row)))
		columns.context = max(columns.context, terminal.Width(columnText(row.Context)))
		columns.primary = max(columns.primary, terminal.Width(columnText(row.Primary)))
		columns.annotation = max(columns.annotation, terminal.Width(columnText(row.Annotation)))
		columns.path = max(columns.path, terminal.Width(columnText(row.Secondary)))
		columns.trailing = max(columns.trailing, commandStatusWidth+terminal.Width(columnText(row.Trailing)))
	}
	return columns
}

func (c columnWidths) fit(width int) columnWidths {
	gapWidth := len(columnGap)
	separatorWidth := terminal.Width(contextSeparator)
	c.trailing = min(c.trailing, width, max(1, width/3))
	c.annotation = min(c.annotation, max(0, width/4))
	available := max(0, width-c.trailing-gapWidth-c.prefix-separatorWidth)
	if c.annotation > 0 {
		available = max(0, available-c.annotation-gapWidth)
	}

	pathReserve := min(c.path, available/2)
	if pathReserve < minColumnPathWidth {
		pathReserve = 0
	}
	labelBudget := available
	if pathReserve > 0 {
		labelBudget = max(0, labelBudget-pathReserve-gapWidth)
	}

	c.primary = min(c.primary, labelBudget-min(c.context, labelBudget/2))
	c.context = min(c.context, labelBudget-c.primary)
	remaining := available - c.context - c.primary
	if c.path > 0 && remaining >= minColumnPathWidth+gapWidth {
		c.path = remaining - gapWidth
	} else {
		c.path = 0
	}
	return c
}

func renderColumnRow(row rowProps, columns columnWidths, width int, styles theme) string {
	style := styles.itemStyle(row.Active, row.Selected)
	muted := styles.meta.Inherit(style)
	prefix := padColumn(renderRowPrefix(row), columns.prefix)
	line := padColumn(columnText(row.Context), columns.context) +
		muted.Render(contextSeparator) + style.Render(padColumn(columnText(row.Primary), columns.primary))
	if columns.annotation > 0 {
		line += muted.Render(columnGap + padColumn(columnText(row.Annotation), columns.annotation))
	}
	if columns.path > 0 {
		pathStyle := styles.secondary
		if row.Selected {
			pathStyle = styles.secondarySelected
		}
		path := shortenPath(columnText(row.Secondary), columns.path)
		line += pathStyle.Inherit(style).Render(columnGap + padColumn(path, columns.path))
	}

	bodyWidth := max(0, width-columns.trailing-len(columnGap))
	prefix = terminal.Cut(prefix, 0, bodyWidth)
	line = prefix + truncateWidth(line, max(0, bodyWidth-terminal.Width(prefix)))
	trailing := renderTrailing(row, columns.trailing, styles)
	padding := max(0, width-columns.trailing-terminal.Width(line))
	line += style.Render(strings.Repeat(" ", padding)) + trailing
	row.JumpLabel = terminal.Cut(displayJumpLabel(row.JumpLabel), 0, max(0, bodyWidth-2))
	return styleRowLine(row, line, width, styles)
}

func columnText(value string) string {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		value = terminal.Sanitize(value)
	}
	return strings.TrimSpace(value)
}

func padColumn(value string, width int) string {
	value = truncateWidth(value, width)
	return value + strings.Repeat(" ", max(0, width-terminal.Width(value)))
}
