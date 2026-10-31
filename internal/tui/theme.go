package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type theme struct {
	palette           Palette
	meta              lipgloss.Style
	scopeActive       lipgloss.Style
	searchBox         lipgloss.Style
	headerHint        lipgloss.Style
	row               lipgloss.Style
	context           lipgloss.Style
	contextSelected   lipgloss.Style
	command           lipgloss.Style
	commandSelected   lipgloss.Style
	metaSelected      lipgloss.Style
	secondary         lipgloss.Style
	secondarySelected lipgloss.Style
	activeRow         lipgloss.Style
	selectedRow       lipgloss.Style
	filterPrompt      lipgloss.Style
	menuKey           lipgloss.Style
	sectionLine       lipgloss.Style
	appBorder         lipgloss.Style
	modal             lipgloss.Style
	modalTitle        lipgloss.Style
	modalHint         lipgloss.Style
}

func (t theme) itemStyle(active, selected bool) lipgloss.Style {
	if selected {
		style := t.selectedRow.Inherit(t.row)
		if active && !style.GetReverse() {
			style = t.activeRow.Inherit(style)
		}

		return style
	}

	style := t.row
	if active {
		style = t.activeRow
	}

	return style
}

func defaultTheme() theme {
	return newTheme(DefaultPalette())
}

func newTheme(p Palette) theme {
	text := lipgloss.NewStyle().Foreground(p.Text)
	muted := mutedStyle(p.Muted, p.Text)
	secondary := mutedStyle(p.Secondary, p.Text)
	command := lipgloss.NewStyle().Foreground(p.Command)

	emphasisColor := p.Emphasis
	if isTerminalDefault(emphasisColor) {
		emphasisColor = p.Text
	}

	emphasis := lipgloss.NewStyle().Foreground(emphasisColor)
	accent := lipgloss.NewStyle().Bold(true).Foreground(p.Accent)
	selection := selectionStyle(p)
	context := text
	contextSelected := selection
	if !isTerminalDefault(p.Context) {
		context = lipgloss.NewStyle().Foreground(p.Context)
		contextSelected = selectedDetailStyle(context, selection)
	}

	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Border)

	return theme{
		palette:           p,
		meta:              muted,
		scopeActive:       lipgloss.NewStyle().Foreground(p.Active).Underline(true),
		searchBox:         emphasis,
		headerHint:        mutedStyle(p.Hint, p.Text).Italic(true),
		row:               text,
		context:           context,
		contextSelected:   contextSelected,
		command:           command,
		commandSelected:   selectedDetailStyle(command, selection),
		metaSelected:      selectedDetailStyle(muted, selection),
		secondary:         secondary.Italic(true).Bold(false),
		secondarySelected: selectedDetailStyle(secondary, selection).Italic(true),
		activeRow:         lipgloss.NewStyle().Bold(true).Foreground(p.Active),
		selectedRow:       selection,
		filterPrompt:      lipgloss.NewStyle().Bold(true).Foreground(p.Active),
		menuKey:           accent,
		sectionLine:       mutedStyle(p.Divider, p.Text),
		appBorder:         border.Padding(0, 2),
		modal:             border.Padding(0, 1),
		modalTitle:        accent,
		modalHint:         muted,
	}
}

func mutedStyle(c, text color.Color) lipgloss.Style {
	if isTerminalDefault(c) {
		return lipgloss.NewStyle().Foreground(text).Faint(true)
	}

	return lipgloss.NewStyle().Foreground(c)
}

func selectionStyle(p Palette) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(p.Text).Background(p.SelectionBG).Bold(true)
	if isTerminalDefault(p.SelectionFG) && isTerminalDefault(p.SelectionBG) {
		return style.Reverse(true)
	}

	if !isTerminalDefault(p.SelectionFG) {
		style = style.Foreground(p.SelectionFG)
	}

	return style
}

func (t theme) annotationStyle(active, selected bool) lipgloss.Style {
	if selected {
		return t.metaSelected
	}

	return t.meta.Inherit(t.itemStyle(active, false))
}

// Inverse selection must not reverse each semantic foreground into a different
// background. A normal selected surface can retain the quieter detail colors.
func selectedDetailStyle(detail, selection lipgloss.Style) lipgloss.Style {
	if selection.GetReverse() {
		return selection
	}

	return detail.Inherit(selection).Bold(false)
}

func (t theme) contextStyle(active, selected bool) lipgloss.Style {
	if active {
		return t.itemStyle(true, selected)
	}

	if selected {
		return t.contextSelected
	}

	return t.context
}

func (t theme) commandStyle(active, selected bool) lipgloss.Style {
	if active {
		return t.itemStyle(true, selected)
	}

	if selected {
		return t.commandSelected
	}

	return t.command
}

func (t theme) primaryStyle(row rowProps) lipgloss.Style {
	if row.Context == "" && row.Depth == 0 {
		style := t.contextStyle(row.Active, row.Selected).Inherit(t.itemStyle(row.Active, row.Selected))
		if row.Selected {
			style = style.Bold(t.selectedRow.GetBold())
		}

		return style
	}

	return t.itemStyle(row.Active, row.Selected)
}

func (t theme) renderPrimaryLabel(row rowProps, label string) string {
	if row.Active || (row.Selected && !t.selectedRow.GetReverse()) {
		return t.primaryStyle(row).Render(label)
	}

	return label
}

func (t theme) withMonochromeSelection() theme {
	native := lipgloss.NoColor{}
	selection := lipgloss.NewStyle().Foreground(native).Background(native).Bold(true).Reverse(true)
	t.selectedRow = selection
	t.secondarySelected = selection.Italic(true)
	t.contextSelected = selection
	t.commandSelected = selection
	t.metaSelected = selection

	return t
}
