package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func requireSelectedBackground(t *testing.T, line string, width int, styles theme) {
	t.Helper()

	screen := lipgloss.NewCanvas(width, 1)
	uv.NewStyledString(line).Draw(screen, screen.Bounds())
	for x := 0; x < width; {
		cell := screen.CellAt(x, 0)
		require.Equal(t, styles.selectedRow.GetBackground(), cell.Style.Bg, "cell %d", x)
		x += max(1, cell.Width)
	}
}

func stackedTreeSnapshot(sessions, windows int) list.Snapshot {
	var snapshot list.Snapshot
	for s := range sessions {
		root := list.Item{ID: list.ItemID{ID: fmt.Sprintf("session-%d", s)}, Primary: fmt.Sprintf("session-%d", s)}
		for w := range windows {
			name := fmt.Sprintf("window-%d-%d", s, w)
			root.Children = append(root.Children, list.Item{
				ID: list.ItemID{Parent: root.ID.ID, ID: name}, Primary: name,
				Secondary: fmt.Sprintf("~/work/%d/%d", s, w), Trailing: "pi",
				SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: name}},
			})
		}
		snapshot.Items = append(snapshot.Items, root)
	}
	return snapshot
}

func TestStackedTreeWindowFitsUnicodeAndStatus(t *testing.T) {
	styles := defaultTheme()
	row := rowProps{
		Primary: "日本é\x1b]0;bad\a", Secondary: "~/長い/path\nnext", Trailing: "pi\t",
		Depth: 1, TreePrefix: "└─ ", Active: true, Selected: true,
		Status: status.Value{State: status.Blocked},
	}
	for _, width := range []int{1, 2, 8, 24, 55, 56} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			lines := renderList(listProps{Width: width, Rows: []rowProps{row}, Theme: styles})

			wantLines := 2
			if width == 56 {
				wantLines = 1
			}
			require.Len(t, lines, wantLines)
			require.Equal(t, "?", terminal.Cut(terminal.Sanitize(lines[0]), width-1, width))
			for _, line := range lines {
				require.Equal(t, width, terminal.Width(line))
				require.NotContains(t, line, "\x1b]")
				require.False(t, strings.ContainsAny(line, "\n\r\t"))
				requireSelectedBackground(t, line, width, styles)
			}
		})
	}
}

func TestTreePreviewAndFilterPreserveSelectionAndRowLayout(t *testing.T) {
	m := browseModel(newModel(stackedTreeSnapshot(2, 3), nil))
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 12})
	m, _ = updateModel(t, m, printableKey("/"))
	m, _ = updateModel(t, m, tea.PasteMsg{Content: "window-1-2"})
	selected := selectedNodeID(m)
	require.Equal(t, list.ItemID{Parent: "session-1", ID: "window-1-2"}, selected)
	require.Len(t, strings.Split(m.viewList(m.layout()), "\n"), 2)

	m, _ = updateModel(t, m, previewKey())

	lines := strings.Split(m.viewList(m.layout()), "\n")
	require.Len(t, lines, 3)
	require.Contains(t, terminal.Sanitize(lines[0]), "session-1")
	require.Contains(t, terminal.Sanitize(lines[1]), "│  window-1-2")
	require.NotContains(t, terminal.Sanitize(lines[1]), "└─")
	require.True(t, strings.HasSuffix(terminal.Sanitize(lines[1]), "pi  "))
	require.Contains(t, terminal.Sanitize(lines[2]), "└─ ~/work/1/2")
	require.Equal(t, selected, selectedNodeID(m))

	m, _ = updateModel(t, m, previewKey())

	require.Len(t, strings.Split(m.viewList(m.layout()), "\n"), 2)
	require.Equal(t, selected, selectedNodeID(m))
	require.Equal(t, "window-1-2", m.items.Query())
}

func TestNarrowTreeStacksWindowsButNotSessionHeadings(t *testing.T) {
	m := browseModel(newModel(stackedTreeSnapshot(1, 2), nil))
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 44, Height: 12})
	m, _ = updateModel(t, m, specialKey(tea.KeyDown))

	lines := strings.Split(m.viewList(m.layout()), "\n")

	require.Len(t, lines, 5)
	require.Contains(t, terminal.Sanitize(lines[0]), "▾ session-0")
	require.True(t, strings.HasPrefix(terminal.Sanitize(lines[1]), "  │  window-0-0"))
	require.True(t, strings.HasSuffix(terminal.Sanitize(lines[1]), "pi  "))
	require.NotContains(t, terminal.Sanitize(lines[1]), "~/work")
	require.Equal(t, "  ├─ ~/work/0/0", strings.TrimRight(terminal.Sanitize(lines[2]), " "))
	require.True(t, strings.HasPrefix(terminal.Sanitize(lines[3]), "  │  window-0-1"))
	require.Equal(t, "  └─ ~/work/0/1", strings.TrimRight(terminal.Sanitize(lines[4]), " "))

	for _, line := range lines[1:3] {
		requireSelectedBackground(t, line, m.layout().listWidth, m.theme)
	}
}

func TestTreeViewportKeepsWholeRowsAcrossPagingAndPreview(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		preview       bool
	}{
		{name: "narrow", width: 44, height: 10},
		{name: "preview", width: 100, height: 10, preview: true},
		{name: "wide", width: 80, height: 10},
		{name: "single line", width: 40, height: 3},
		{name: "two lines", width: 40, height: 4},
		{name: "short framed", width: 40, height: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := browseModel(newModel(stackedTreeSnapshot(4, 3), nil))
			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			if tc.preview {
				m, _ = updateModel(t, m, previewKey())
			}

			for _, key := range []tea.KeyPressMsg{
				specialKey(tea.KeyDown), specialKey(tea.KeyPgDown), printableKey("G"),
				specialKey(tea.KeyPgUp), printableKey("g"), printableKey("H"), printableKey("L"),
			} {
				m, _ = updateModel(t, m, key)

				require.Contains(t, visibleIDs(m), selectedNodeID(m))
				require.LessOrEqual(t, lipgloss.Height(m.View().Content), tc.height)
				require.LessOrEqual(t, lipgloss.Height(m.viewList(m.layout())), m.layout().middleHeight)
			}
		})
	}
}
