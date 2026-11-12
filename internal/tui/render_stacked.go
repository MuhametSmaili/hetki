package tui

import (
	"strings"

	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// Narrow rows put identity and command/status above the path.
// A one-line viewport falls back to columns rather than overflowing.
const narrowListWidth = 56

func stackedRowHeight(row list.Row) int {
	props := rowProps{
		Context:   row.Item.Context,
		Secondary: row.Item.Secondary,
		Trailing:  row.Item.Trailing,
		Depth:     row.Depth,
		Branch:    row.Branch,
	}
	return props.stackedHeight()
}

func (row rowProps) stackedHeight() int {
	if row.Context == "" && (row.Depth == 0 || row.Branch && row.Secondary == "" && row.Trailing == "") {
		return 1
	}
	return 2
}

func usesStackedRows(props listProps) bool {
	if props.Width >= narrowListWidth || props.Height == 1 {
		return false
	}

	for _, row := range props.measureRows() {
		if row.Context != "" || row.Depth > 0 {
			return true
		}
	}
	return false
}

func renderStackedList(props listProps) []string {
	trailingWidth := 0
	for _, row := range props.measureRows() {
		trailingWidth = max(trailingWidth, rowTrailingWidth(row))
	}

	trailingWidth = min(trailingWidth, props.Width, max(1, props.Width/3))

	lines := make([]string, 0, 2*len(props.Rows))
	for _, row := range props.Rows {
		if row.stackedHeight() == 1 {
			line := truncateWidth(renderRowLine(row, props.Compact), props.Width)
			line = props.Theme.renderPrimaryLabel(row, line)
			lines = append(lines, styleRowLine(row, line, props.Width, props.Theme))
			continue
		}
		lines = append(lines,
			stackedIdentityLine(row, trailingWidth, props.Width, props.Theme),
			stackedDetailLine(row, props.Width, props.Theme),
		)
	}
	return lines
}

func stackedIdentityLine(row rowProps, trailingWidth, width int, styles theme) string {
	if rowTrailingWidth(row) == 0 {
		trailingWidth = 0
	}
	bodyWidth := width
	if trailingWidth > 0 {
		bodyWidth = max(0, width-trailingWidth-len(columnGap))
	}

	prefix := terminal.Cut(renderRowPrefix(row), 0, bodyWidth)
	style := styles.itemStyle(row.Active, row.Selected)
	muted := styles.annotationStyle(row.Active, row.Selected)
	identity := row
	identity.TreePrefix = strings.ReplaceAll(row.TreePrefix, "├─", "│ ")
	identity.TreePrefix = strings.ReplaceAll(identity.TreePrefix, "└─", "│ ")
	label := styles.renderPrimaryLabel(row, decoratedLabel(identity, true))
	if row.Context != "" {
		contextStyle := styles.contextStyle(row.Active, row.Selected)
		label = contextStyle.Render(columnText(row.Context)) + muted.Render(contextSeparator) + style.Render(columnText(row.Primary))
	}
	if annotation := columnText(row.Annotation); annotation != "" {
		label += muted.Render(columnGap + annotation)
	}

	budget := max(0, bodyWidth-terminal.Width(prefix))
	line := prefix + truncateWidth(label, budget)
	if trailingWidth > 0 {
		padding := max(0, width-terminal.Width(line)-trailingWidth)
		line += style.Render(strings.Repeat(" ", padding)) + renderTrailing(row, trailingWidth, styles)
	}
	return styleRowLine(row, line, width, styles)
}

func stackedDetailLine(row rowProps, width int, styles theme) string {
	style := styles.itemStyle(row.Active, row.Selected)
	pathStyle := styles.secondary.Inherit(style)
	if row.Selected {
		pathStyle = styles.secondarySelected.Inherit(style)
	}

	indent := renderRowPrefix(row) + row.TreePrefix
	indent = terminal.Cut(indent, 0, width)
	budget := max(0, width-terminal.Width(indent))
	line := indent + pathStyle.Render(shortenPath(columnText(row.Secondary), budget))
	return styleRowLine(row, truncateWidth(line, width), width, styles)
}
