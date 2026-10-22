package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
)

func TestSearchBarRendersModeAppropriateContent(t *testing.T) {
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("2"))
	query := lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	got := renderSearchBar(searchBarProps{
		Width: 32, Prompt: " / ", Meta: renderHeaderMeta(headerMetaProps{Right: "3/3"}),
		Theme: theme{searchBox: query, searchPrompt: prompt},
	})
	if !strings.Contains(got, prompt.Render(" / ")) || terminal.Width(got) != 32 {
		t.Fatalf("search shortcut header = %q", got)
	}

	active := renderSearchBar(searchBarProps{
		Width: 20, Prompt: " / ", Filter: "dev", Active: true,
		Theme: theme{searchBox: query, filterPrompt: prompt},
	})
	if !strings.Contains(active, query.Render(" dev█")) {
		t.Fatalf("active query used the wrong styling: %q", active)
	}
	if plain := strings.TrimRight(terminal.Sanitize(active), " "); plain != " /  dev█" {
		t.Fatalf("active search header = %q", plain)
	}

	hintStyle := lipgloss.NewStyle().Italic(true)
	centered := renderSearchBar(searchBarProps{
		Width: 32, Hint: "press / to filter", Meta: renderHeaderMeta(headerMetaProps{Right: "3/3"}),
		Theme: theme{headerHint: hintStyle},
	})
	if !strings.Contains(centered, hintStyle.Render("press / to filter")) {
		t.Fatalf("centered hint did not use its style: %q", centered)
	}
	plain := terminal.Sanitize(centered)
	if strings.HasPrefix(strings.TrimLeft(plain, " "), "/") || strings.Index(plain, "press / to filter") != (32-len("press / to filter"))/2 {
		t.Fatalf("normal header = %q", plain)
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
			renderSearchBar(searchBarProps{
				Width: width, Filter: unsafe, Meta: renderHeaderMeta(headerMetaProps{Right: unsafe}),
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
