package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestPreviewStartsBesideHeaderAndPreservesList(t *testing.T) {
	for _, mode := range []uiMode{modeBrowse, modeFilter, modeJump} {
		t.Run(string(mode), func(t *testing.T) {
			m := previewModel(t)
			m.theme.appBorder = lipgloss.NewStyle()
			m.width, m.height, m.mode = 100, 12, mode
			m.preview.visible, m.preview.ready = true, true
			m.preview.itemID = list.ItemID{ID: "a"}
			m = m.reflow()
			layout := m.layout()
			m.preview.lines = []string{"older row"}
			for i := range layout.middleHeight + 2 {
				m.preview.lines = append(m.preview.lines, fmt.Sprintf("preview %d", i))
			}
			listLines := strings.Split(m.viewList(layout), "\n")

			lines := strings.Split(m.View().Content, "\n")

			require.Len(t, lines, m.height)
			for i, line := range lines {
				require.Equal(t, m.width, terminal.Width(line))
				require.Equal(t, " │ ", terminal.Sanitize(terminal.Cut(line, layout.listWidth, layout.listWidth+3)))
				preview := terminal.Sanitize(terminal.Cut(line, layout.listWidth+3, m.width))
				require.Equal(t, fmt.Sprintf("preview %d", i), strings.TrimRight(preview, " "))
			}
			meta := terminal.Sanitize(terminal.Cut(lines[1], 0, layout.listWidth))
			require.Equal(t, []string{"All", "Tracked", "3"}, strings.Fields(meta))
			for i, line := range listLines {
				expected := lipgloss.NewStyle().Width(layout.listWidth).Render(line)
				actual := terminal.Cut(lines[i+2], 0, layout.listWidth)
				require.Equal(t, terminal.Sanitize(expected), terminal.Sanitize(actual))
			}
		})
	}
}

func TestPreviewHeaderSeparatesQueryFromMetadata(t *testing.T) {
	for _, width := range []int{48, 60, 120, 200} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := previewModel(t)
			m.width, m.mode, m.preview.visible = width, modeFilter, true
			m.items.SetQuery("a-very-long-query-that-does-not-fit-in-the-list")
			m = m.reflow()
			layout := m.layout()

			lines := strings.Split(m.View().Content, "\n")

			query := terminal.Sanitize(lines[1])
			meta := terminal.Sanitize(lines[2])
			require.Contains(t, query, "> a-very-long")
			require.NotContains(t, query, "FILTER")
			require.NotContains(t, query, "Tracked")
			require.Contains(t, meta, "All  Tracked  0/3")
			for _, line := range lines {
				require.LessOrEqual(t, terminal.Width(line), width)
			}
			require.GreaterOrEqual(t, layout.previewWidth, 20)
		})
	}
}

func TestPreviewHeaderDiagnosticsStaySanitizedAndClipped(t *testing.T) {
	m := previewModel(t)
	m.width, m.preview.visible = 48, true
	m.err = fmt.Errorf("a long failure\x1b]0;secret\a\nnext")
	m = m.reflow()

	lines := strings.Split(m.View().Content, "\n")

	require.Contains(t, terminal.Sanitize(lines[2]), "All  Tracked")
	require.Contains(t, terminal.Sanitize(lines[2]), "a l...")
	require.NotContains(t, strings.Join(lines, "\n"), "secret")
	for _, line := range lines {
		require.LessOrEqual(t, terminal.Width(line), m.width)
	}
}
