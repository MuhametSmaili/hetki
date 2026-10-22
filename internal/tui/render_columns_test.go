package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestViewAlignsColumnsAcrossStatusAndFilterChanges(t *testing.T) {
	items := []list.Item{
		{
			ID: list.ItemID{ID: "first"}, Context: "hetki", Primary: "1:node", Annotation: "·0", Secondary: "~/code", Trailing: "pi",
			SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "pi"}},
		},
		{ID: list.ItemID{ID: "second"}, Context: "日本", Primary: "12:node", Annotation: "·1", Secondary: "~/other", Trailing: "opencode"},
		{ID: list.ItemID{ID: "shell"}, Context: "other", Primary: "3:zsh", Secondary: "~/code"},
	}
	pane := status.PaneRef{Server: "server", ID: "pane", Generation: "birth"}
	m := newModel(list.Snapshot{Items: items, ActiveItemID: items[0].ID}, nil)
	m.width, m.height = 100, 12
	m = m.reflow()
	m.paneStatus.options.Bindings = StatusBindings{items[0].ID: {pane}, items[2].ID: {pane}}
	rightEdge := m.layout().listWidth
	for _, state := range []status.State{status.Withdrawn, status.Working, status.Blocked} {
		values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
			pane: {State: state, ValidUntil: time.Now().Add(time.Minute)},
		})
		require.NoError(t, err)
		m.paneStatus.values = values

		lines := strings.Split(m.viewList(m.layout()), "\n")
		for i := range lines {
			lines[i] = terminal.Sanitize(lines[i])
		}

		require.Len(t, lines, 3)
		require.Contains(t, lines[0], "hetki")
		require.Contains(t, lines[1], "日本")
		require.Equal(t, "1:node ", terminal.Cut(lines[0], 10, 17))
		require.Equal(t, "12:node", terminal.Cut(lines[1], 10, 17))
		require.Equal(t, "·0", terminal.Cut(lines[0], 19, 21))
		require.Equal(t, "·1", terminal.Cut(lines[1], 19, 21))
		require.Equal(t, "~/code", terminal.Cut(lines[0], 23, 29))
		require.Equal(t, "~/other", terminal.Cut(lines[1], 23, 30))
		require.Equal(t, "pi", terminal.Cut(lines[0], rightEdge-4, rightEdge-2))
		require.Equal(t, "opencode  ", terminal.Cut(lines[1], rightEdge-10, rightEdge))
		if state == status.Blocked {
			require.Equal(t, "pi ?", terminal.Cut(lines[0], rightEdge-4, rightEdge))
			require.Equal(t, "?", terminal.Cut(lines[2], rightEdge-1, rightEdge))
		}

		m.items.SetQuery("pi")
		filtered := strings.Split(m.viewList(m.layout()), "\n")

		require.Len(t, filtered, 1)
		require.Equal(t, "pi ", terminal.Cut(terminal.Sanitize(filtered[0]), rightEdge-4, rightEdge-1))
		m.items.SetQuery("")
	}

	m.paneStatus.options.Bindings[items[1].ID] = []status.PaneRef{pane}
	for _, test := range []struct {
		width int
		state status.State
		want  string
	}{
		{width: 12, state: status.Withdrawn, want: "op  "},
		{width: 12, state: status.Blocked, want: "op ?"},
		{width: 24, state: status.Withdrawn, want: "ope...  "},
		{width: 24, state: status.Blocked, want: "ope... ?"},
	} {
		t.Run(fmt.Sprintf("narrow %d/%s", test.width, test.state), func(t *testing.T) {
			values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
				pane: {State: test.state, ValidUntil: time.Now().Add(time.Minute)},
			})
			require.NoError(t, err)
			m.paneStatus.values = values

			lines := strings.Split(m.viewList(layoutMetrics{listWidth: test.width}), "\n")

			// Narrow flat rows stack; the second row's detail line is line 3.
			detail := terminal.Sanitize(lines[3])
			require.Equal(t, test.want, terminal.Cut(detail, test.width-terminal.Width(test.want), test.width))
		})
	}
}

func TestColumnRowsFitAndKeepSelectedBackground(t *testing.T) {
	styles := defaultTheme()
	row := rowProps{
		Context: "\x1b]0;bad\a日本👨‍👩‍👧", Primary: "1:éditor\nnext", Annotation: "·10\t",
		Secondary: "~/long/path\x1b[2J", Trailing: "pi\r", JumpLabel: "a",
		Active: true, Selected: true, Status: status.Value{State: status.Blocked},
	}
	for width := range 81 {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			lines := renderList(listProps{Width: width, Rows: []rowProps{row}, Theme: styles})

			if width < narrowListWidth {
				require.Len(t, lines, 2)
			} else {
				require.Len(t, lines, 1)
			}

			if width > 0 {
				last := terminal.Sanitize(lines[len(lines)-1])
				require.Equal(t, "?", terminal.Cut(last, width-1, width))
			}

			for _, line := range lines {
				require.Equal(t, width, terminal.Width(line))
				require.NotContains(t, line, "\x1b]")
				require.False(t, strings.ContainsAny(line, "\n\r\t"))
				screen := lipgloss.NewCanvas(width, 1)
				uv.NewStyledString(line).Draw(screen, screen.Bounds())
				for x := 0; x < width; {
					cell := screen.CellAt(x, 0)
					require.Equal(t, styles.selectedRow.GetBackground(), cell.Style.Bg, "cell %d", x)
					x += max(1, cell.Width)
				}
			}
		})
	}

	line := renderList(listProps{Width: 80, Rows: []rowProps{row}, Theme: styles})[0]
	screen := lipgloss.NewCanvas(80, 1)
	uv.NewStyledString(line).Draw(screen, screen.Bounds())
	require.Equal(t, "│", screen.CellAt(0, 0).Content)
	require.Equal(t, lipgloss.Color(colorActive), screen.CellAt(0, 0).Style.Fg)
	require.Equal(t, "?", screen.CellAt(79, 0).Content)
	require.Equal(t, lipgloss.Color(colorBlocked), screen.CellAt(79, 0).Style.Fg)
	annotationColumn := terminal.Width(strings.Split(terminal.Sanitize(line), "·10")[0])
	require.Equal(t, lipgloss.Color(colorMuted), screen.CellAt(annotationColumn, 0).Style.Fg)
}

func TestFlatListReservesStatusBeforeJumpLabels(t *testing.T) {
	row := rowProps{Context: "hetki", Primary: "tui", JumpLabel: "ab", Status: status.Value{State: status.Blocked}}
	for _, test := range []struct {
		name     string
		height   int
		selected bool
	}{
		{name: "stack", height: 2},
		{name: "selected stack", height: 2, selected: true},
		{name: "single-line fallback", height: 1},
		{name: "selected fallback", height: 1, selected: true},
	} {
		for width := 1; width <= 10; width++ {
			t.Run(fmt.Sprintf("%s width %d", test.name, width), func(t *testing.T) {
				row.Selected = test.selected

				lines := renderList(listProps{Width: width, Height: test.height, Rows: []rowProps{row}, Theme: defaultTheme()})

				last := terminal.Sanitize(lines[len(lines)-1])
				require.Equal(t, width, terminal.Width(last))
				require.Equal(t, "?", terminal.Cut(last, width-1, width))
				if test.height == 2 {
					prefix := terminal.Cut("  ab ", 0, width)
					require.True(t, strings.HasPrefix(terminal.Sanitize(lines[0]), prefix))
				} else if width >= 8 {
					require.Equal(t, "  ab", terminal.Cut(terminal.Sanitize(lines[0]), 0, 4))
				}
			})
		}
	}
}

func TestStackedListKeepsIdentityAndDetailLines(t *testing.T) {
	styles := defaultTheme()
	rows := []rowProps{
		{Context: "hetki", Primary: "tui", Annotation: "·1", Secondary: "~/Projects/hetki/internal/tui", Trailing: "opencode", JumpLabel: "a", Active: true},
		{Context: "api", Primary: "server", Secondary: "~/Projects/api/internal/server", Trailing: "go test", Status: status.Value{State: status.Blocked}, Selected: true},
	}

	stacked := renderList(listProps{Width: narrowListWidth - 1, Rows: rows, Theme: styles})
	require.Len(t, stacked, 4)

	identity, detail := terminal.Sanitize(stacked[0]), terminal.Sanitize(stacked[1])
	require.Equal(t, "│ a hetki › tui  ·1", identity)
	require.Equal(t, "│   ", terminal.Cut(detail, 0, 4))
	require.Contains(t, detail, "~/Projects/hetki/internal/tui")
	require.True(t, strings.HasSuffix(detail, "opencode  "))

	selected := terminal.Sanitize(stacked[3])
	require.True(t, strings.HasSuffix(selected, "go test ?"))
	require.Equal(t, narrowListWidth-1, terminal.Width(selected))

	tight := renderList(listProps{Width: 30, Rows: rows, Theme: styles})
	require.Contains(t, terminal.Sanitize(tight[1]), "~/.../tui")

	wide := renderList(listProps{Width: narrowListWidth, Rows: rows, Theme: styles})
	require.Len(t, wide, 2)
}

func TestFlatListFitsShortViewport(t *testing.T) {
	for _, height := range []int{3, 4, 5, 6, 7, 24} {
		t.Run(fmt.Sprintf("height %d", height), func(t *testing.T) {
			snapshot := flatJumpSnapshot(20)
			for i := range snapshot.Items {
				snapshot.Items[i].Context = "hetki"
				snapshot.Items[i].Trailing = "pi"
			}
			m := browseModel(newModel(snapshot, nil))

			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 40, Height: height})
			m, _ = updateModel(t, m, specialKey(tea.KeyPgDown))

			layout := m.layout()
			require.LessOrEqual(t, lipgloss.Height(m.View().Content), height)
			require.LessOrEqual(t, lipgloss.Height(m.viewList(layout)), layout.middleHeight)
			require.Equal(t, m.availableListHeight(), m.items.Cursor())
			require.Contains(t, visibleIDs(m), selectedNodeID(m))
			if layout.middleHeight == 1 {
				require.Contains(t, terminal.Sanitize(m.viewList(layout)), "hetki")
				require.True(t, strings.HasSuffix(terminal.Sanitize(m.viewList(layout)), "pi  "))
			}
		})
	}
}

func TestStackedFlatListHalvesVisibleRows(t *testing.T) {
	flatItems := make([]list.Item, 12)
	treeItems := make([]list.Item, 12)
	for i := range flatItems {
		id := list.ItemID{ID: fmt.Sprintf("pane-%d", i)}
		flatItems[i] = list.Item{ID: id, Context: "hetki", Primary: fmt.Sprintf("w%d", i), Secondary: "~/code", Trailing: "sh"}
		treeItems[i] = list.Item{ID: id, Primary: fmt.Sprintf("w%d", i), Secondary: "~/code", Trailing: "sh"}
	}

	m := browseModel(newModel(list.Snapshot{Items: flatItems}, nil))
	m.width, m.height = 40, 24
	m = m.reflow()

	layout := m.layout()
	require.True(t, m.stackedList(layout))
	require.Equal(t, 10, m.availableListHeight())
	require.Len(t, m.items.VisibleRows(), 10)
	require.Len(t, strings.Split(m.viewList(layout), "\n"), 20)

	tree := browseModel(newModel(list.Snapshot{Items: treeItems}, nil))
	tree.width, tree.height = 40, 24
	tree = tree.reflow()

	require.False(t, tree.stackedList(tree.layout()))
	require.Equal(t, 20, tree.availableListHeight())
	require.Len(t, tree.items.VisibleRows(), 12)
}
