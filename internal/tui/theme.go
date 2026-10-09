package tui

import "charm.land/lipgloss/v2"

const (
	colorText             = "252" // #d0d0d0 — primary text
	colorMuted            = "245" // #8a8a8a — counts and modal hints
	colorHeaderHint       = "238" // #444444 — low-priority header instructions
	colorSecondary        = "245" // #8a8a8a — readable paths
	colorEmphasis         = "255" // #eeeeee — focused text
	colorActive           = "217" // #ffafaf — warm pink
	colorAccent           = "110" // #87afd7 — soft blue
	colorBlocked          = "214"
	colorIdle             = "114"
	colorSelection        = "238" // #444444 — quiet selection
	colorPromptText       = "235" // #262626 — dark prompt text
	colorPromptBackground = "181" // #d7afaf — dusty rose
	colorDivider          = "237" // #3a3a3a — subtle separators
)

type theme struct {
	meta              lipgloss.Style
	searchBox         lipgloss.Style
	headerHint        lipgloss.Style
	row               lipgloss.Style
	secondary         lipgloss.Style
	secondarySelected lipgloss.Style
	activeRow         lipgloss.Style
	selectedRow       lipgloss.Style
	searchPrompt      lipgloss.Style
	jumpLabel         lipgloss.Style
	sectionLine       lipgloss.Style
	appBorder         lipgloss.Style
	modal             lipgloss.Style
	modalTitle        lipgloss.Style
	modalHint         lipgloss.Style
}

func (t theme) itemStyle(active, selected bool) lipgloss.Style {
	style := t.row
	if active {
		style = t.activeRow
	}
	if selected {
		style = t.selectedRow.Inherit(style)
	}
	return style
}

func defaultTheme() theme {
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	secondary := lipgloss.NewStyle().Foreground(lipgloss.Color(colorSecondary))
	emphasis := lipgloss.NewStyle().Foreground(lipgloss.Color(colorEmphasis))
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent))
	selection := lipgloss.NewStyle().Background(lipgloss.Color(colorSelection))
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(colorAccent))
	return theme{
		meta:              muted,
		searchBox:         emphasis,
		headerHint:        lipgloss.NewStyle().Foreground(lipgloss.Color(colorHeaderHint)).Italic(true),
		row:               text,
		secondary:         secondary.Italic(true),
		secondarySelected: emphasis.Inherit(selection).Italic(true),
		activeRow:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorActive)),
		selectedRow:       selection,
		searchPrompt:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorPromptText)).Background(lipgloss.Color(colorPromptBackground)),
		jumpLabel:         accent,
		sectionLine:       lipgloss.NewStyle().Foreground(lipgloss.Color(colorDivider)),
		appBorder:         border.Padding(0, 2),
		modal:             border.Padding(0, 1),
		modalTitle:        accent,
		modalHint:         muted,
	}
}
