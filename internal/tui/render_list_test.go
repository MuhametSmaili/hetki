package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestShortenPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		maxW int
	}{
		{name: "fits unchanged", path: "~/code", maxW: 20},
		{name: "elides middle keeping tail", path: "~/if_it_is_long/the-end", maxW: 16},
		{name: "very tight width", path: "~/a/b/c/d/e", maxW: 4},
		{name: "tail longer than width falls back", path: "~/short/averylongfinalsegment", maxW: 10},
		{name: "CJK emoji and combining marks", path: "~/项目/👨‍👩‍👧/éditor", maxW: 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortenPath(tt.path, tt.maxW)
			if w := lipgloss.Width(got); w > tt.maxW {
				t.Fatalf("shortenPath(%q, %d) = %q, width %d exceeds max", tt.path, tt.maxW, got, w)
			}
		})
	}
}

func TestShortenPathKeepsFinalSegmentWhenItFits(t *testing.T) {
	got := shortenPath("~/if_it_is_long/the-end", 16)
	if !strings.HasSuffix(got, "/the-end") {
		t.Fatalf("expected the final segment to be preserved, got %q", got)
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected an ellipsis in the middle, got %q", got)
	}
}

func TestShortenPathPreservesRootAndAsManyFinalComponentsAsFit(t *testing.T) {
	got := shortenPath("~/dev/a-very-long-component/internal/tui", 22)
	if got != "~/.../internal/tui" {
		t.Fatalf("expected rooted component elision, got %q", got)
	}
}

func TestShortenPathZeroWidth(t *testing.T) {
	if got := shortenPath("~/code", 0); got != "" {
		t.Fatalf("expected empty string for zero width, got %q", got)
	}
}

func TestRenderListUsesPresentationFallbackForEmptyPrimary(t *testing.T) {
	lines := renderList(listProps{Width: 20, Rows: []rowProps{{}}})

	if plain := terminal.Sanitize(lines[0]); plain != "  -" {
		t.Fatalf("empty primary fallback = %q, want %q", plain, "  -")
	}
}

func TestRenderListUsesFullWidthSelectionAndCompactIndicators(t *testing.T) {
	selected := lipgloss.NewStyle().Background(lipgloss.Color("1"))
	secondarySelected := lipgloss.NewStyle().Background(lipgloss.Color("1")).Italic(true)
	theme := theme{
		secondary:         lipgloss.NewStyle().Italic(true),
		secondarySelected: secondarySelected,
		selectedRow:       selected,
	}
	got := renderList(listProps{Width: 24, Compact: true, Theme: theme, Rows: []rowProps{{
		Primary: "editor", Secondary: "~/code", Active: true, Selected: true,
	}}})[0]
	require.Equal(t, 24, terminal.Width(got))
	requireSelectedStyle(t, got, 24, theme)
	path := renderedTextCell(t, []string{got}, 24, "~/code")
	require.NotZero(t, path.Style.Attrs&uv.AttrItalic)
	if plain := strings.TrimRight(terminal.Sanitize(got), " "); plain != "│ editor  ~/code" {
		t.Fatalf("selected row has excess markers or spacing: %q", plain)
	}
}

func TestRenderListMakesOnlyTheActiveFlatRowBold(t *testing.T) {
	theme := theme{
		row:       lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		activeRow: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2")),
	}
	lines := renderList(listProps{Width: 24, Compact: true, Theme: theme, Rows: []rowProps{
		{Primary: "inactive"},
		{Primary: "active", Active: true},
	}})
	require.Equal(t, "  inactive", terminal.Sanitize(lines[0]))
	require.Equal(t, "│ active", terminal.Sanitize(lines[1]))
	inactive := renderedTextCell(t, lines[:1], 24, "inactive")
	active := renderedTextCell(t, lines[1:], 24, "active")
	require.Equal(t, lipgloss.Color("1"), inactive.Style.Fg)
	require.Zero(t, inactive.Style.Attrs&uv.AttrBold)
	require.Equal(t, lipgloss.Color("2"), active.Style.Fg)
	require.NotZero(t, active.Style.Attrs&uv.AttrBold)
}

func TestRenderListDefaultThemePreservesHierarchyAcrossRowShapes(t *testing.T) {
	theme := defaultTheme()
	const width = 24
	shapes := []struct {
		name     string
		row      rowProps
		normal   string
		active   string
		selected string
	}{
		{
			name: "flat", row: rowProps{Primary: "flat"},
			normal: "  flat", active: "│ flat", selected: "  flat",
		},
		{
			name: "tree root", row: rowProps{Primary: "root", Branch: true},
			normal: "  ▸ root", active: "│ ▸ root", selected: "  ▸ root",
		},
		{
			name: "tree child", row: rowProps{Primary: "child", Depth: 1, TreePrefix: "└─ "},
			normal: "    └─ child", active: "│   └─ child", selected: "    └─ child",
		},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			active := shape.row
			active.Active = true
			selected := shape.row
			selected.Selected = true
			selectedActive := active
			selectedActive.Selected = true

			normalLines := renderList(listProps{Width: width, Height: 1, Rows: []rowProps{shape.row}, Theme: theme})
			activeLines := renderList(listProps{Width: width, Height: 1, Rows: []rowProps{active}, Theme: theme})
			selectedLines := renderList(listProps{Width: width, Height: 1, Rows: []rowProps{selected}, Theme: theme})
			selectedActiveLines := renderList(listProps{Width: width, Height: 1, Rows: []rowProps{selectedActive}, Theme: theme})

			require.Equal(t, shape.normal, terminal.Sanitize(normalLines[0]))
			require.Equal(t, shape.active, terminal.Sanitize(activeLines[0]))
			require.Equal(t, shape.selected, strings.TrimRight(terminal.Sanitize(selectedLines[0]), " "))
			require.Nil(t, renderedTextCell(t, normalLines, width, shape.row.Primary).Style.Fg)
			activeCell := renderedTextCell(t, activeLines, width, shape.row.Primary)
			require.Equal(t, theme.palette.Active, activeCell.Style.Fg)
			require.NotZero(t, activeCell.Style.Attrs&uv.AttrBold)
			selectedCell := renderedTextCell(t, selectedLines, width, shape.row.Primary)
			require.Equal(t, theme.palette.SelectionFG, selectedCell.Style.Fg)
			require.NotZero(t, selectedCell.Style.Attrs&uv.AttrBold)
			require.Equal(t, width, terminal.Width(selectedLines[0]))
			requireSelectedStyle(t, selectedLines[0], width, theme)
			selectedActiveCell := renderedTextCell(t, selectedActiveLines, width, shape.row.Primary)
			require.Equal(t, theme.palette.Active, selectedActiveCell.Style.Fg)
			require.NotZero(t, selectedActiveCell.Style.Attrs&uv.AttrBold)
			requireSelectedStyle(t, selectedActiveLines[0], width, theme)
		})
	}

	path := theme.secondarySelected.Render("~/code")
	require.Equal(t, theme.palette.Secondary, theme.secondarySelected.GetForeground())
	require.Equal(t, theme.selectedRow.GetBackground(), theme.secondarySelected.GetBackground())
	require.Equal(t, theme.selectedRow.GetReverse(), theme.secondarySelected.GetReverse())
	line := renderList(listProps{Width: width, Rows: []rowProps{{Primary: "flat", Secondary: "~/code", Selected: true}}, Theme: theme})[0]
	if !strings.Contains(line, path) {
		t.Fatalf("selected path lost its contrast style: %q", line)
	}
}

func TestRenderListAlignsSecondaryColumnsAndTruncatesBothSides(t *testing.T) {
	lines := renderList(listProps{Width: 48, Rows: []rowProps{
		{Primary: "dev > editor", Secondary: "~/code"},
		{Primary: "production > logs", Secondary: "/var/log"},
	}})
	if strings.Index(lines[0], "~/code") != strings.Index(lines[1], "/var/log") {
		t.Fatalf("secondary columns are not aligned: %#v", lines)
	}

	compact := renderList(listProps{Width: 40, Compact: true, Rows: []rowProps{
		{Primary: "dev > editor", Secondary: "~/code"},
		{Primary: "production > logs", Secondary: "/var/log"},
	}})
	if !strings.Contains(compact[0], "~/code") || strings.Index(compact[0], "~/code") != strings.Index(compact[1], "/var/log") {
		t.Fatalf("compact flat rows lost aligned paths: %#v", compact)
	}

	line := renderList(listProps{Width: 24, Rows: []rowProps{{
		Primary: "a-very-long-session > a-very-long-window", Secondary: "~/a/very/long/final-path",
	}}})[0]
	if !strings.Contains(line, "...") || !strings.HasSuffix(line, "ath") {
		t.Fatalf("expected independent middle path elision, got %q", line)
	}
	if width := lipgloss.Width(line); width > 24 {
		t.Fatalf("rendered width %d exceeds 24: %q", width, line)
	}
}

func TestRenderListSanitizesAndFitsNarrowWidths(t *testing.T) {
	row := rowProps{Primary: "\x1b]0;owned\a中👨‍👩‍👧é\nnext", Secondary: "~/bad\tpath\x1b[2J", Trailing: "\x1b]0;owned\api中\ncommand"}
	for width := range 81 {
		line := renderList(listProps{Width: width, Rows: []rowProps{row}})[0]
		if strings.ContainsAny(line, "\x1b\n\r\t") {
			t.Fatalf("width %d left terminal controls in %q", width, line)
		}
		if actual := lipgloss.Width(line); actual > width {
			t.Fatalf("width %d rendered %q at width %d", width, line, actual)
		}
	}
}

func TestRenderListAlignsTrailingTextBeforeTheStatusCell(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, width := range []int{40, 64, 100} {
			t.Run(fmt.Sprintf("compact=%t/width=%d", compact, width), func(t *testing.T) {
				rows := []rowProps{
					{Primary: "deveditor", Secondary: "~/code", Trailing: "pi", Selected: true},
					{Primary: "devtests", Secondary: "~/tests", Trailing: "opencode", Active: true},
				}

				lines := renderList(listProps{Width: width, Rows: rows, Compact: compact, Theme: defaultTheme()})

				for i, line := range lines {
					plain := terminal.Sanitize(line)
					require.Equal(t, width, terminal.Width(plain))
					require.True(t, strings.HasSuffix(plain, rows[i].Trailing+"  "))
					require.Contains(t, plain, rows[i].Secondary)
				}
				require.Equal(t,
					terminal.Width(strings.Split(terminal.Sanitize(lines[0]), "~/code")[0]),
					terminal.Width(strings.Split(terminal.Sanitize(lines[1]), "~/tests")[0]),
				)
			})
		}
	}
}

func TestRenderListPreservesCommandWhileShorteningOtherColumns(t *testing.T) {
	line := renderList(listProps{Width: 40, Rows: []rowProps{{
		Primary: "a-very-long-sessiona-very-long-window", Secondary: "~/a/very/long/path/src", Trailing: "opencode",
	}}})[0]
	if !strings.HasSuffix(line, "opencode  ") || !strings.Contains(line, "...") || terminal.Width(line) != 40 {
		t.Fatalf("expected independently fitted columns: %q", line)
	}
	line = renderList(listProps{Width: 12, Rows: []rowProps{{Primary: "dev", Trailing: "a-very-long-command"}}})[0]
	if !strings.Contains(line, "dev") || terminal.Width(line) != 12 {
		t.Fatalf("long command consumed the label or overflowed: %q", line)
	}
}

func TestSelectedRowBackgroundCoversEveryCellThroughTheCommand(t *testing.T) {
	styles := defaultTheme()
	for _, compact := range []bool{false, true} {
		for _, active := range []bool{false, true} {
			for _, width := range []int{12, 24, 40, 80} {
				line := renderList(listProps{Width: width, Compact: compact, Theme: styles, Rows: []rowProps{
					{Primary: "dev editor", Secondary: "~/code", Trailing: "pi", Selected: true, Active: active},
					{Primary: "dev tests", Secondary: "~/tests", Trailing: "opencode"},
				}})[0]
				require.NotContains(t, line, "\n")
				require.Equal(t, width, terminal.Width(line), "selected row must occupy one full-width line")
				requireSelectedStyle(t, line, width, styles)
			}
		}
	}
}
