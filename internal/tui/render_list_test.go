package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
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

func TestRenderListShowsJumpLabelBesideTheRow(t *testing.T) {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	line := renderList(listProps{
		Width: 40, Theme: theme{jumpLabel: labelStyle},
		Rows: []rowProps{{Primary: "editor", JumpLabel: "aa"}},
	})[0]
	if plain := terminal.Sanitize(line); plain != "  aa editor" {
		t.Fatalf("jump label was not placed beside its row: %q", plain)
	}
	if _, noColor := defaultTheme().jumpLabel.GetBackground().(lipgloss.NoColor); !noColor {
		t.Fatal("jump labels must not form a continuous background column")
	}
	if width := terminal.Width(labelStyle.Render(displayJumpLabel("a"))); width != 1 {
		t.Fatalf("single-character jump label width = %d, want one cell", width)
	}
}

func TestRenderListUsesFullWidthSelectionAndCompactIndicators(t *testing.T) {
	selected := lipgloss.NewStyle().Background(lipgloss.Color("1"))
	secondarySelected := lipgloss.NewStyle().Background(lipgloss.Color("1")).Italic(true)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	theme := theme{
		secondary:         lipgloss.NewStyle().Italic(true),
		secondarySelected: secondarySelected,
		selectedRow:       selected,
		jumpLabel:         labelStyle,
	}
	got := renderList(listProps{Width: 24, Compact: true, Theme: theme, Rows: []rowProps{{
		Primary: "editor", Secondary: "~/code", JumpLabel: "a", Active: true, Selected: true,
	}}})[0]
	want := selected.Width(24).Render("│ a editor  " + secondarySelected.Render("~/code"))
	if got != want {
		t.Fatalf("selected row was not styled as one full-width line:\n got %q\nwant %q", got, want)
	}
	if plain := strings.TrimRight(terminal.Sanitize(got), " "); plain != "│ a editor  ~/code" {
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
	if lines[0] != theme.row.Render("  inactive") {
		t.Fatalf("inactive flat row used the wrong typography: %q", lines[0])
	}
	if lines[1] != theme.activeRow.Render("│ active") {
		t.Fatalf("active flat row used the wrong typography: %q", lines[1])
	}
}

func TestRenderListDefaultThemeStylesEveryRowShapeTheSame(t *testing.T) {
	theme := defaultTheme()
	const width = 24
	shapes := []struct {
		name     string
		row      rowProps
		normal   string
		active   string
		selected string
	}{
		{name: "flat", row: rowProps{Primary: "flat"}, normal: "  flat", active: "│ flat", selected: "  flat"},
		{
			name: "tree root", row: rowProps{Primary: "root", Branch: true},
			normal: "  \uf0da root", active: "│ \uf0da root", selected: "  \uf0da root",
		},
		{
			name: "tree child", row: rowProps{Primary: "child", Depth: 1, TreePrefix: "└─ "},
			normal: "    └─ child", active: "│   └─ child", selected: "    └─ child",
		},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			if got := renderList(listProps{Width: width, Rows: []rowProps{shape.row}, Theme: theme})[0]; got != theme.row.Render(shape.normal) {
				t.Fatalf("normal row style = %q", got)
			}
			active := shape.row
			active.Active = true
			if got := renderList(listProps{Width: width, Rows: []rowProps{active}, Theme: theme})[0]; got != theme.activeRow.Render(shape.active) {
				t.Fatalf("active row style = %q", got)
			}
			selected := shape.row
			selected.Selected = true
			want := theme.selectedRow.Inherit(theme.row).Width(width).Render(shape.selected)
			if got := renderList(listProps{Width: width, Rows: []rowProps{selected}, Theme: theme})[0]; got != want {
				t.Fatalf("selected row style = %q, want %q", got, want)
			}
		})
	}

	path := theme.secondarySelected.Render("~/code")
	if foreground := fmt.Sprint(theme.secondarySelected.GetForeground()); foreground != colorEmphasis {
		t.Fatalf("selected path foreground = %q, want %q", foreground, colorEmphasis)
	}
	if background := fmt.Sprint(theme.secondarySelected.GetBackground()); background != colorSelection {
		t.Fatalf("selected path background = %q, want %q", background, colorSelection)
	}
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
	if !strings.Contains(line, "...") || !strings.Contains(line, "path") {
		t.Fatalf("expected independent middle path elision, got %q", line)
	}
	if width := lipgloss.Width(line); width > 24 {
		t.Fatalf("rendered width %d exceeds 24: %q", width, line)
	}
}

func TestRenderListSanitizesAndFitsNarrowWidths(t *testing.T) {
	row := rowProps{Primary: "\x1b]0;owned\a中👨‍👩‍👧é\nnext", Secondary: "~/bad\tpath\x1b[2J", Trailing: "\x1b]0;owned\api中\ncommand", JumpLabel: "\x1b[31ma\n"}
	for width := 0; width <= 80; width++ {
		line := renderList(listProps{Width: width, Rows: []rowProps{row}})[0]
		if strings.ContainsAny(line, "\x1b\n\r\t") {
			t.Fatalf("width %d left terminal controls in %q", width, line)
		}
		if actual := lipgloss.Width(line); actual > width {
			t.Fatalf("width %d rendered %q at width %d", width, line, actual)
		}
	}
}

func TestRenderListAlignsTrailingTextAtTheRightEdge(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, width := range []int{40, 64, 100} {
			rows := []rowProps{
				{Primary: "deveditor", Secondary: "~/code", Trailing: "pi", Selected: true},
				{Primary: "devtests", Secondary: "~/tests", Trailing: "opencode", Active: true, JumpLabel: "a"},
			}
			lines := renderList(listProps{Width: width, Rows: rows, Compact: compact, Theme: defaultTheme()})
			for i, line := range lines {
				plain := terminal.Sanitize(line)
				if terminal.Width(plain) != width || !strings.HasSuffix(plain, rows[i].Trailing) {
					t.Fatalf("width %d: trailing text is not at the right edge: %q", width, plain)
				}
				if !strings.Contains(plain, rows[i].Secondary) {
					t.Fatalf("width %d: path disappeared: %q", width, plain)
				}
			}
			if terminal.Width(strings.Split(terminal.Sanitize(lines[0]), "~/code")[0]) != terminal.Width(strings.Split(terminal.Sanitize(lines[1]), "~/tests")[0]) {
				t.Fatalf("paths lost their aligned middle column: %#v", lines)
			}
		}
	}
}

func TestRenderListPreservesCommandWhileShorteningOtherColumns(t *testing.T) {
	line := renderList(listProps{Width: 40, Rows: []rowProps{{
		Primary: "a-very-long-sessiona-very-long-window", Secondary: "~/a/very/long/path/src", Trailing: "opencode",
	}}})[0]
	if !strings.HasSuffix(line, "opencode") || !strings.Contains(line, "...") || terminal.Width(line) != 40 {
		t.Fatalf("expected independently fitted columns: %q", line)
	}
	line = renderList(listProps{Width: 12, Rows: []rowProps{{Primary: "dev", Trailing: "a-very-long-command"}}})[0]
	if !strings.Contains(line, "dev") || terminal.Width(line) != 12 {
		t.Fatalf("long command consumed the label or overflowed: %q", line)
	}
}

func TestSelectedRowBackgroundCoversEveryCellThroughTheCommand(t *testing.T) {
	styles := defaultTheme()
	wantR, wantG, wantB, wantA := styles.selectedRow.GetBackground().RGBA()
	for _, compact := range []bool{false, true} {
		for _, active := range []bool{false, true} {
			for _, width := range []int{12, 24, 40, 80} {
				line := renderList(listProps{Width: width, Compact: compact, Theme: styles, Rows: []rowProps{
					{Primary: "dev editor", Secondary: "~/code", Trailing: "pi", JumpLabel: "a", Selected: true, Active: active},
					{Primary: "dev tests", Secondary: "~/tests", Trailing: "opencode"},
				}})[0]
				if strings.Contains(line, "\n") || terminal.Width(line) != width {
					t.Fatalf("selected row does not occupy one full-width line: %q", line)
				}
				screen := uv.NewScreenBuffer(width, 1)
				uv.NewStyledString(line).Draw(screen, screen.Bounds())
				for x := range width {
					cell := screen.CellAt(x, 0)
					if cell.Style.Bg == nil {
						t.Fatalf("width %d: highlight stops at cell %d (%q): %q", width, x, cell.Content, line)
					}
					r, g, b, a := cell.Style.Bg.RGBA()
					if r != wantR || g != wantG || b != wantB || a != wantA {
						t.Fatalf("width %d: wrong highlight at cell %d (%q)", width, x, cell.Content)
					}
				}
			}
		}
	}
}
