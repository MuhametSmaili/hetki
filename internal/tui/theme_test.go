package tui

import (
	"fmt"
	"image/color"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestDefaultThemeKeepsNativeTextWithCuratedSelection(t *testing.T) {
	styles := defaultTheme()

	require.Equal(t, lipgloss.NoColor{}, styles.row.GetForeground())
	require.Equal(t, lipgloss.NoColor{}, styles.row.GetBackground())
	require.Equal(t, lipgloss.Color("#79c0ff"), styles.menuKey.GetForeground())
	require.False(t, styles.meta.GetFaint())
	require.False(t, styles.selectedRow.GetReverse())
	require.Equal(t, lipgloss.Color("#ffffff"), styles.selectedRow.GetForeground())
	require.Equal(t, lipgloss.Color("#1c2d44"), styles.selectedRow.GetBackground())
	require.True(t, styles.selectedRow.GetBold())
}

func TestContextFollowsTextUnlessOverridden(t *testing.T) {
	native := lipgloss.NoColor{}
	text := lipgloss.Color("#abcdef")
	context := lipgloss.Color("#123456")
	active := lipgloss.Color("#fedcba")
	selection := DefaultPalette().SelectionFG

	for _, test := range []struct {
		name        string
		text        color.Color
		context     color.Color
		selected    bool
		active      bool
		wantContext color.Color
		wantPrimary color.Color
	}{
		{"native", native, native, false, false, nil, nil},
		{"native selected", native, native, true, false, selection, selection},
		{"custom text", text, native, false, false, text, text},
		{"custom text selected", text, native, true, false, selection, selection},
		{"context override", text, context, false, false, context, text},
		{"selected context override", text, context, true, false, context, selection},
		{"selected active with context override", text, context, true, true, active, active},
	} {
		t.Run(test.name, func(t *testing.T) {
			palette := DefaultPalette()
			palette.Text = test.text
			palette.Context = test.context
			palette.Active = active
			styles := newTheme(palette)
			row := rowProps{Context: "project", Primary: "editor", Selected: test.selected, Active: test.active}

			lines := renderList(listProps{Width: 120, Rows: []rowProps{row}, Theme: styles})

			require.Equal(t, test.wantContext, renderedTextCell(t, lines, 120, "project").Style.Fg)
			require.Equal(t, test.wantPrimary, renderedTextCell(t, lines, 120, "editor").Style.Fg)
		})
	}
}

func TestHexSelectionCoversEveryRowSegment(t *testing.T) {
	palette := DefaultPalette()
	palette.Text = lipgloss.Color("#abcdef")
	palette.Muted = lipgloss.Color("#112233")
	palette.Active = lipgloss.Color("#ff0000")
	palette.Blocked = lipgloss.Color("#00ff00")
	palette.SelectionFG = lipgloss.Color("#fedcba")
	palette.SelectionBG = lipgloss.Color("#123456")
	styles := newTheme(palette)

	for _, shape := range []struct {
		name string
		row  rowProps
	}{
		{"flat", rowProps{Primary: "editor", Secondary: "~/code"}},
		{"columns", rowProps{Context: "project", Primary: "editor", Annotation: "·2", Secondary: "~/code"}},
		{"tree", rowProps{Primary: "editor", Depth: 1, TreePrefix: "└─ ", Secondary: "~/code"}},
	} {
		for _, width := range []int{1, 32, 80} {
			t.Run(fmt.Sprintf("%s/width=%d", shape.name, width), func(t *testing.T) {
				row := shape.row
				row.Active, row.Selected = true, true
				row.Trailing, row.Status = "pi", status.Value{State: status.Blocked}

				lines := renderList(listProps{Width: width, Rows: []rowProps{row}, Theme: styles})

				for _, line := range lines {
					require.Equal(t, width, terminal.Width(line))
					requireSelectedStyle(t, line, width, styles)
				}
			})
		}
	}
}

func TestSelectionPartialOverridesUseNormalPair(t *testing.T) {
	for _, test := range []struct {
		name   string
		fg, bg color.Color
		wantFG color.Color
		wantBG color.Color
	}{
		{"foreground only", lipgloss.Color("#000000"), lipgloss.NoColor{}, lipgloss.Color("#000000"), nil},
		{"background only", lipgloss.NoColor{}, lipgloss.Color("#123456"), lipgloss.Color("#abcdef"), lipgloss.Color("#123456")},
	} {
		t.Run(test.name, func(t *testing.T) {
			palette := DefaultPalette()
			palette.Text = lipgloss.Color("#abcdef")
			palette.SelectionFG, palette.SelectionBG = test.fg, test.bg
			styles := newTheme(palette)
			screen := lipgloss.NewCanvas(1, 1)

			uv.NewStyledString(styles.itemStyle(false, true).Render("x")).Draw(screen, screen.Bounds())

			cell := screen.CellAt(0, 0)
			require.Equal(t, test.wantFG, cell.Style.Fg)
			require.Equal(t, test.wantBG, cell.Style.Bg)
			require.Zero(t, cell.Style.Attrs&uv.AttrReverse)
		})
	}
}

func TestMutedHexOverridesReplaceFaint(t *testing.T) {
	palette := DefaultPalette()
	custom := lipgloss.Color("#89b4fa")
	palette.Muted, palette.Hint, palette.Secondary, palette.Divider = custom, custom, custom, custom
	styles := newTheme(palette)

	for _, style := range []lipgloss.Style{styles.meta, styles.headerHint, styles.secondary, styles.sectionLine} {
		require.Equal(t, custom, style.GetForeground())
		require.False(t, style.GetFaint())
	}
}

func TestStatusGlyphColorsUseResolvedPalette(t *testing.T) {
	palette := DefaultPalette()
	palette.Working = lipgloss.Color("#112233")
	palette.Blocked = lipgloss.Color("#223344")
	palette.Idle = lipgloss.Color("#334455")
	palette.Success = lipgloss.Color("#445566")
	palette.Error = lipgloss.Color("#556677")
	palette.Muted = lipgloss.Color("#667788")
	styles := newTheme(palette)

	for _, test := range []struct {
		name  string
		value status.Value
		want  color.Color
	}{
		{"working", status.Value{State: status.Working}, palette.Working},
		{"blocked", status.Value{State: status.Blocked}, palette.Blocked},
		{"idle", status.Value{State: status.Idle}, palette.Idle},
		{"success", status.Value{State: status.Unknown, Outcome: status.Completed}, palette.Success},
		{"error", status.Value{State: status.Unknown, Outcome: status.Failed}, palette.Error},
		{"unknown", status.Value{State: status.Unknown}, palette.Muted},
	} {
		t.Run(test.name, func(t *testing.T) {
			screen := lipgloss.NewCanvas(1, 1)

			line := renderTrailing(rowProps{Status: test.value, StatusTime: time.Time{}}, 1, styles)
			uv.NewStyledString(line).Draw(screen, screen.Bounds())

			require.Equal(t, test.want, screen.CellAt(0, 0).Style.Fg)
		})
	}
}

func TestThemeColorsReachHeaderAndDialogChrome(t *testing.T) {
	palette := DefaultPalette()
	palette.Border, palette.Accent = lipgloss.Color("#123456"), lipgloss.Color("#abcdef")
	palette.Emphasis, palette.Active = lipgloss.Color("#fedcba"), lipgloss.Color("#654321")
	styles := newTheme(palette)

	require.Equal(t, palette.Border, styles.appBorder.GetBorderTopForeground())
	require.Equal(t, palette.Border, styles.modal.GetBorderTopForeground())
	require.Equal(t, palette.Accent, styles.modalTitle.GetForeground())
	require.Equal(t, palette.Emphasis, styles.searchBox.GetForeground())
	require.Equal(t, palette.Active, styles.filterPrompt.GetForeground())
	require.Equal(t, palette.Active, styles.scopeActive.GetForeground())
}

func TestThemeDoesNotRecolorCapturedPreview(t *testing.T) {
	m := previewModel(t)
	palette := DefaultPalette()
	palette.Text, palette.Secondary = lipgloss.Color("#123456"), lipgloss.Color("#abcdef")
	m.theme = newTheme(palette)
	m.preview.ready = true
	m.preview.request.ItemID = m.items.VisibleRows()[0].Item.ID
	m.preview.lines = terminal.ScreenLines("\x1b[31mRED PREVIEW\x1b[0m")
	screen := lipgloss.NewCanvas(20, 1)

	preview := m.viewPreview(20, 1)
	uv.NewStyledString(preview).Draw(screen, screen.Bounds())

	require.Contains(t, preview, "RED PREVIEW")
	require.NotContains(t, preview, "38;2;18;52;86")
	require.Equal(t, lipgloss.Red, screen.CellAt(0, 0).Style.Fg)
}

func TestNativeSelectionHasNoColoredPatches(t *testing.T) {
	row := rowProps{
		Context:    "project",
		Primary:    "editor",
		Annotation: "·2",
		Secondary:  "~/code",
		Trailing:   "pi",
		Active:     true,
		Selected:   true,
		Status:     status.Value{State: status.Blocked},
	}
	styles := defaultTheme().withMonochromeSelection()

	for _, width := range []int{1, 32, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			lines := renderList(listProps{Width: width, Rows: []rowProps{row}, Theme: styles})

			for _, line := range lines {
				screen := lipgloss.NewCanvas(width, 1)
				uv.NewStyledString(line).Draw(screen, screen.Bounds())
				for x := range width {
					cell := screen.CellAt(x, 0)
					require.Nil(t, cell.Style.Fg, "foreground at cell %d", x)
					require.Nil(t, cell.Style.Bg, "background at cell %d", x)
					require.NotZero(t, cell.Style.Attrs&uv.AttrReverse, "selection at cell %d", x)
				}
			}
		})
	}
}

func TestMonochromeSelectionRestoresInverseWithCustomColors(t *testing.T) {
	palette := DefaultPalette()
	palette.SelectionFG, palette.SelectionBG = lipgloss.Color("#abcdef"), lipgloss.Color("#123456")
	styles := newTheme(palette)

	styles = styles.withMonochromeSelection()

	require.True(t, styles.selectedRow.GetReverse())
	require.True(t, styles.selectedRow.GetBold())
	require.True(t, styles.secondarySelected.GetReverse())
	require.Equal(t, lipgloss.NoColor{}, styles.selectedRow.GetForeground())
	require.Equal(t, lipgloss.NoColor{}, styles.selectedRow.GetBackground())
}
