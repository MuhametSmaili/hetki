package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func paneTreeSnapshot() list.Snapshot {
	return list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "dev"}, Primary: "dev", Children: []list.Item{{
			ID: list.ItemID{Parent: "dev", ID: "editor"}, Primary: "editor", Children: []list.Item{
				{ID: list.ItemID{Scope: "dev", Parent: "editor", ID: "pane-0"}, Primary: "0", Secondary: "~/Projects/hetki", Trailing: "nvim"},
				{ID: list.ItemID{Scope: "dev", Parent: "editor", ID: "pane-1"}, Primary: "1", Secondary: "~/Projects/hetki", Trailing: "opencode",
					SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "opencode"}}},
			},
		}},
	}}}
}

func TestPaneTreeKeepsHeadingsCompactAndStatusOnPaneRows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   int
		preview bool
		lines   int
		command string
	}{
		{name: "wide", width: 100, lines: 4, command: "opencode ?"},
		{name: "narrow", width: 44, lines: 6, command: "opencode ?"},
		{name: "preview", width: 100, preview: true, lines: 6, command: "open... ?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(paneTreeSnapshot(), nil)
			m.width, m.height, m.preview.enabled = tc.width, 14, tc.preview
			m = m.reflow()
			paneID := list.ItemID{Scope: "dev", Parent: "editor", ID: "pane-1"}
			ref := status.PaneRef{Server: "server", ID: "pane-1", Generation: "birth"}
			m.paneStatus.options.Bindings = StatusBindings{paneID: {ref}}
			values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
				ref: {State: status.Blocked, ValidUntil: time.Now().Add(time.Minute)},
			})
			require.NoError(t, err)
			m.paneStatus.values = values
			require.True(t, m.items.Select(paneID))

			lines := strings.Split(m.viewList(m.layout()), "\n")
			for i := range lines {
				lines[i] = terminal.Sanitize(lines[i])
			}

			require.Len(t, lines, tc.lines)
			require.Contains(t, lines[0], "dev")
			require.Contains(t, lines[1], "editor")
			require.NotContains(t, lines[1], "?")
			require.Contains(t, strings.Join(lines[2:], "\n"), "nvim")
			require.Contains(t, strings.Join(lines[2:], "\n"), tc.command)
			for _, line := range lines {
				require.LessOrEqual(t, terminal.Width(line), m.layout().listWidth)
			}

			m.setQuery("opencode")

			require.Len(t, m.items.Rows(), 3)
			require.Equal(t, paneID, selectedNodeID(m))
			require.LessOrEqual(t, lipgloss.Height(m.viewList(m.layout())), m.layout().middleHeight)
		})
	}
}

func TestPaneTreePagingFitsShortViewports(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		preview       bool
	}{
		{name: "narrow", width: 44, height: 7},
		{name: "preview", width: 100, height: 8, preview: true},
		{name: "one line", width: 40, height: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := browseModel(newModel(paneTreeSnapshot(), nil))
			m.width, m.height, m.preview.enabled = tc.width, tc.height, tc.preview
			m = m.reflow()

			for _, key := range []tea.KeyPressMsg{
				specialKey(tea.KeyDown), specialKey(tea.KeyPgDown), printableKey("G"), specialKey(tea.KeyPgUp), printableKey("g"),
			} {
				m, _ = updateModel(t, m, key)

				require.Contains(t, visibleIDs(m), selectedNodeID(m))
				require.LessOrEqual(t, lipgloss.Height(m.View().Content), tc.height)
				require.LessOrEqual(t, lipgloss.Height(m.viewList(m.layout())), m.layout().middleHeight)
			}
		})
	}
}
