package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

func TestSearchBarRendersModeAppropriateContent(t *testing.T) {
	hintStyle := lipgloss.NewStyle().Italic(true)
	query := lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styles := defaultTheme()
	styles.headerHint = hintStyle
	browse := renderSearchBar(searchBarProps{
		Width: 32, Prompt: ">", Hint: "press / to filter", Meta: renderHeaderMeta(headerMetaProps{Right: "3/3"}),
		Theme: styles,
	})

	if !strings.HasPrefix(browse, styles.filterPrompt.Render(">")) {
		t.Fatalf("browse header should keep the accent prompt: %q", browse)
	}
	if !strings.Contains(browse, hintStyle.Render(" press / to filter")) {
		t.Fatalf("browse hint should follow the prompt with its muted style: %q", browse)
	}
	if terminal.Width(browse) != 32 || !strings.HasSuffix(terminal.Sanitize(browse), "3/3") {
		t.Fatalf("browse metadata should remain right-aligned: %q", browse)
	}

	active := renderSearchBar(searchBarProps{
		Width: 20, Prompt: ">", Filter: "dev", Active: true,
		Theme: theme{searchBox: query, filterPrompt: defaultTheme().filterPrompt},
	})

	if !strings.Contains(active, query.Render(" dev█")) {
		t.Fatalf("active query used the wrong styling: %q", active)
	}
	if plain := strings.TrimRight(terminal.Sanitize(active), " "); plain != "> dev█" {
		t.Fatalf("active search header = %q", plain)
	}

	retained := renderSearchBar(searchBarProps{
		Width: 20, Prompt: ">", Filter: "dev", Theme: defaultTheme(),
	})

	if plain := strings.TrimRight(terminal.Sanitize(retained), " "); plain != "> dev" {
		t.Fatalf("retained query should keep its prompt without a cursor: %q", plain)
	}

	emptyFilter := renderSearchBar(searchBarProps{
		Width: 20, Prompt: ">", Hint: "press / to filter", Active: true, Theme: styles,
	})

	if plain := terminal.Sanitize(emptyFilter); plain != "> █" {
		t.Fatalf("empty filter should show its cursor, not the hint: %q", plain)
	}
}

func TestHeaderRulePadsItsLabelByOneCell(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
		label string
		want  string
	}{
		{name: "count", width: 8, label: "6", want: "6 ──────"},
		{name: "filtered count", width: 8, label: "2/6", want: "2/6 ────"},
		{name: "padded label", width: 8, label: " 6 ", want: "6 ──────"},
		{name: "clip ends on whitespace", width: 4, label: "a long notice", want: "a ──"},
		{name: "empty count", width: 8, want: "────────"},
		{name: "wide label", width: 8, label: "中", want: "中 ─────"},
		{name: "label fills width", width: 4, label: "1234", want: "12 ─"},
		{name: "clipped label", width: 3, label: "a long notice", want: "a ─"},
		{name: "wide label clipped at glyph boundary", width: 4, label: "中x", want: "中 ─"},
		{name: "one cell", width: 1, label: "6", want: "─"},
		{name: "two cells", width: 2, label: "6", want: "──"},
		{name: "glyph cannot fit beside separator", width: 3, label: "中", want: "───"},
		{name: "sanitized label", width: 8, label: "6\x1b]0;owned\a", want: "6 ──────"},
		{name: "zero width"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := renderHeaderRule(tc.width, tc.label, defaultTheme())

			if plain := terminal.Sanitize(line); plain != tc.want {
				t.Fatalf("header rule = %q, want %q", plain, tc.want)
			}
			if actual := terminal.Width(line); actual != tc.width {
				t.Fatalf("header rule width = %d, want %d", actual, tc.width)
			}
		})
	}
}

func TestMenuUsesSharedItemSelectionColors(t *testing.T) {
	styles := defaultTheme()
	menu := renderMenu(menuProps{
		LineWidth: 80,
		Entries: []MenuEntry{
			{Action: ActionOpen, Label: "Open destination"},
			{Action: ActionRefresh, Label: "Refresh"},
		},
		Keys:  DefaultKeyMap(),
		Theme: styles,
	})
	if selected := styles.itemStyle(false, true).Render("‹o› Open destination"); !strings.Contains(menu, selected) {
		t.Fatalf("selected menu entry did not use the list selection colors: %q", menu)
	}
	if !strings.Contains(menu, styles.menuKey.Render("‹ctrl+r› ")) {
		t.Fatalf("unselected menu shortcut lost its accent style: %q", menu)
	}
}

func TestHeadersAndModalsSanitizeAndFit(t *testing.T) {
	unsafe := "中👨‍👩‍👧é\x1b]0;owned\a\nnext"
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())

	for width := range 21 {
		rendered := []string{
			renderHeaderRule(width, unsafe, defaultTheme()),
			renderSearchBar(searchBarProps{Width: width, Prompt: ">", Hint: unsafe, Theme: defaultTheme()}),
			renderSearchBar(searchBarProps{
				Width: width, Filter: unsafe, Hint: unsafe, Meta: renderHeaderMeta(headerMetaProps{Right: unsafe}),
			}),
			renderInputModal(inputModalProps{LineWidth: width, Title: "INPUT", Prompt: unsafe, Value: unsafe, Theme: theme{modal: border}}),
			renderConfirmModal(confirmModalProps{LineWidth: width, Title: "CONFIRM", Body: unsafe, Theme: theme{modal: border}}),
			renderMenu(menuProps{
				LineWidth: width, Title: unsafe, Theme: theme{modal: border},
				Entries: []MenuEntry{{Action: ActionOpen, Label: unsafe}},
			}),
		}
		for _, output := range rendered {
			if strings.Contains(output, "owned") || strings.Contains(output, "\nnext") {
				t.Fatalf("width %d rendered unsafe text: %q", width, output)
			}
			for _, line := range strings.Split(output, "\n") {
				if actual := terminal.Width(line); actual > width {
					t.Fatalf("width %d rendered line width %d: %q", width, actual, line)
				}
			}
		}
	}
}
