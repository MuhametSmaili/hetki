package tui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestCuratedRowHierarchy(t *testing.T) {
	command := lipgloss.Color("#39c5cf")
	current := lipgloss.Color("#58a6ff")
	selection := lipgloss.Color("#ffffff")

	for _, test := range []struct {
		name        string
		width       int
		selected    bool
		active      bool
		wantName    color.Color
		wantCommand color.Color
	}{
		{"stacked", 48, false, false, nil, command},
		{"selected stacked", 48, true, false, selection, command},
		{"columns", 120, false, false, nil, command},
		{"selected columns", 120, true, false, selection, command},
		{"current stacked", 48, false, true, current, current},
		{"current columns", 120, false, true, current, current},
		{"selected current stacked", 48, true, true, current, current},
		{"selected current columns", 120, true, true, current, current},
	} {
		t.Run(test.name, func(t *testing.T) {
			width, selected := test.width, test.selected
			styles := defaultTheme()
			row := rowProps{
				Context:    "project",
				Primary:    "editor",
				Annotation: "·2",
				Secondary:  "~/code",
				Trailing:   "pi",
				Status:     status.Value{State: status.Blocked},
				Selected:   selected,
				Active:     test.active,
			}

			lines := renderList(listProps{Width: width, Rows: []rowProps{row}, Theme: styles})

			for _, segment := range []struct {
				text  string
				color color.Color
			}{
				{"project", test.wantName},
				{"~/code", lipgloss.Color("#8b949e")},
				{"·2", lipgloss.Color("#6e7681")},
				{"pi", test.wantCommand},
				{"?", lipgloss.Color("#d29922")},
			} {
				cell := renderedTextCell(t, lines, width, segment.text)
				require.Equal(t, segment.color, cell.Style.Fg, segment.text)
			}

			path := renderedTextCell(t, lines, width, "~/code")
			label := renderedTextCell(t, lines, width, "editor")
			require.NotZero(t, path.Style.Attrs&uv.AttrItalic)
			require.Zero(t, path.Style.Attrs&uv.AttrBold)
			require.NotEqual(t, label.Style.Fg, path.Style.Fg)
			require.Equal(t, test.wantName, label.Style.Fg)
			if selected {
				for _, line := range lines {
					requireSelectedStyle(t, line, width, styles)
				}
			} else {
				require.Nil(t, path.Style.Bg)
			}
		})
	}
}

func renderedTextCell(t *testing.T, lines []string, width int, text string) *uv.Cell {
	t.Helper()

	for _, line := range lines {
		plain := terminal.Sanitize(line)
		before, _, ok := strings.Cut(plain, text)
		if !ok {
			continue
		}

		screen := lipgloss.NewCanvas(width, 1)
		uv.NewStyledString(line).Draw(screen, screen.Bounds())
		return screen.CellAt(terminal.Width(before), 0)
	}

	t.Fatalf("rendered lines do not contain %q", text)
	return nil
}
