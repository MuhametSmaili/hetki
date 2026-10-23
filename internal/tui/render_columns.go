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

type columnWidths struct {
	prefix     int
	context    int
	primary    int
	annotation int
	path       int
	trailing   int
}

func renderColumnList(props listProps) []string {
	columns := measureColumns(props.Rows).fit(props.Width)
	lines := make([]string, len(props.Rows))
	for i, row := range props.Rows {
		lines[i] = renderColumnRow(row, columns, props.Width, props.Theme)
	}
	return lines
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
