package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestListColumnsStayFixedWhileScrolling(t *testing.T) {
	session := func(name, window string) list.Item {
		return list.Item{
			ID: list.ItemID{ID: name}, Primary: name, Secondary: "~/" + name,
			Children: []list.Item{{ID: list.ItemID{ID: name + "/1"}, Primary: window, Secondary: "~/" + name + "/src", Trailing: "nvim"}},
		}
	}
	items := []list.Item{
		session("api", "1 vim"),
		session("web", "1 zsh"),
		session("db", "1 psql"),
		session("a-much-longer-session-name", "1 a-much-longer-window-name"),
	}

	for _, test := range []struct {
		name  string
		items []list.Item
	}{
		{name: "tree", items: items},
		{name: "columns", items: []list.Item{
			{ID: list.ItemID{ID: "1"}, Context: "api", Primary: "1:vim", Secondary: "~/api"},
			{ID: list.ItemID{ID: "2"}, Context: "web", Primary: "1:zsh", Secondary: "~/web"},
			{ID: list.ItemID{ID: "3"}, Context: "db", Primary: "1:psql", Secondary: "~/db"},
			{ID: list.ItemID{ID: "4"}, Context: "a-much-longer-session-name", Primary: "1:a-much-longer-window-name", Secondary: "~/long"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := newModel(list.Snapshot{Items: test.items}, nil)
			m.width, m.height = 100, 12
			m = m.reflow()
			layout := layoutMetrics{listWidth: 90, middleHeight: 3}
			m.items.Resize(layout.middleHeight)

			top := strings.Split(m.viewList(layout), "\n")
			require.Len(t, top, 3)
			m.items.Bottom()
			bottom := strings.Split(m.viewList(layout), "\n")
			require.Len(t, bottom, 3)
			require.NotEqual(t, top, bottom, "the viewport did not scroll")

			requireSamePathColumn(t, append(top, bottom...))
		})
	}
}

func requireSamePathColumn(t *testing.T, lines []string) {
	t.Helper()
	want := -1
	for _, line := range lines {
		plain := terminal.Sanitize(line)
		for field := range strings.FieldsSeq(plain) {
			if !strings.HasPrefix(field, "~/") {
				continue
			}
			before, _, _ := strings.Cut(plain, field)
			got := terminal.Width(before)
			if want == -1 {
				want = got
			}
			require.Equal(t, want, got, "path column moved in %q", plain)
		}
	}
	require.NotEqual(t, -1, want, "no paths rendered")
}

func TestTreeViewKeepsAlignedPathsAndTrailingStatusWhenFiltered(t *testing.T) {
	windows := []list.Item{
		{ID: list.ItemID{ID: "editor"}, Primary: "1 editor", Secondary: "~/code", Trailing: "pi",
			SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "pi"}}},
		{ID: list.ItemID{ID: "tests"}, Primary: "2 tests", Secondary: "~/tests", Trailing: "opencode"},
	}
	pane := status.PaneRef{Server: "server", ID: "pane", Generation: "birth"}
	m := newModel(list.Snapshot{Items: []list.Item{{ID: list.ItemID{ID: "dev"}, Primary: "dev", Children: windows}}}, nil)
	m.width, m.height = 100, 12
	m = m.reflow()
	m.paneStatus.options.Bindings = StatusBindings{windows[0].ID: {pane}}
	for _, state := range []status.State{status.Withdrawn, status.Working, status.Blocked} {
		values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
			pane: {State: state, ValidUntil: time.Now().Add(time.Minute)},
		})
		require.NoError(t, err)
		m.paneStatus.values = values
		for _, compact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/compact=%t", state, compact), func(t *testing.T) {
				layout := layoutMetrics{listWidth: 80, compact: compact}

				lines := strings.Split(m.viewList(layout), "\n")
				for i := range lines {
					lines[i] = terminal.Sanitize(lines[i])
				}

				require.Len(t, lines, 3)
				require.Contains(t, lines[0], "▾ dev")
				require.Contains(t, lines[1], "├─ 1 editor")
				require.Contains(t, lines[2], "└─ 2 tests")
				require.Contains(t, lines[1], "~/code")
				require.Contains(t, lines[2], "~/tests")
				require.Equal(t, terminal.Width(strings.Split(lines[1], "~/code")[0]), terminal.Width(strings.Split(lines[2], "~/tests")[0]))
				require.Equal(t, "pi ", terminal.Cut(lines[1], 76, 79))
				require.Equal(t, "opencode  ", terminal.Cut(lines[2], 70, 80))

				m.items.SetQuery("pi")
				filtered := strings.Split(m.viewList(layout), "\n")
				for i := range filtered {
					filtered[i] = terminal.Sanitize(filtered[i])
				}

				require.Len(t, filtered, 2)
				require.Contains(t, filtered[1], "└─ 1 editor")
				require.Equal(t, "pi ", terminal.Cut(filtered[1], 76, 79))
				if state == status.Blocked {
					require.Equal(t, "?", terminal.Cut(filtered[1], 79, 80))
				}
				m.items.SetQuery("")
			})
		}
	}
}
