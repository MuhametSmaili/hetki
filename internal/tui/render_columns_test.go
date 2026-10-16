package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

			require.Equal(t, test.want, terminal.Cut(terminal.Sanitize(lines[1]), test.width-terminal.Width(test.want), test.width))
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
	for width := 0; width <= 80; width++ {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			line := renderList(listProps{Width: width, Rows: []rowProps{row}, Theme: styles})[0]

			require.Equal(t, width, terminal.Width(line))
			require.NotContains(t, line, "\x1b]")
			require.False(t, strings.ContainsAny(line, "\n\r\t"))
			screen := uv.NewScreenBuffer(width, 1)
			uv.NewStyledString(line).Draw(screen, screen.Bounds())
			for x := 0; x < width; {
				cell := screen.CellAt(x, 0)
				require.Equal(t, styles.selectedRow.GetBackground(), cell.Style.Bg, "cell %d", x)
				x += max(1, cell.Width)
			}
		})
	}

	line := renderList(listProps{Width: 80, Rows: []rowProps{row}, Theme: styles})[0]
	screen := uv.NewScreenBuffer(80, 1)
	uv.NewStyledString(line).Draw(screen, screen.Bounds())
	require.Equal(t, "│", screen.CellAt(0, 0).Content)
	require.Equal(t, lipgloss.Color(colorActive), screen.CellAt(0, 0).Style.Fg)
	require.Equal(t, "?", screen.CellAt(79, 0).Content)
	require.Equal(t, lipgloss.Color(colorBlocked), screen.CellAt(79, 0).Style.Fg)
	annotationColumn := terminal.Width(strings.Split(terminal.Sanitize(line), "·10")[0])
	require.Equal(t, lipgloss.Color(colorMuted), screen.CellAt(annotationColumn, 0).Style.Fg)
}
