package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func viewSnapshot() list.Snapshot {
	return list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "session"}, Primary: "session", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "session"}},
		Children: []list.Item{{
			ID: list.ItemID{ID: "window"}, Primary: "editor", Secondary: "~/code/editor",
			SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "editor"}, {Tier: list.SearchSecondary, Text: "~/code/editor"}},
		}},
	}}}
}

func flatSnapshot(count int) list.Snapshot {
	items := make([]list.Item, count)
	for i := range items {
		name := fmt.Sprintf("item %02d", i)
		items[i] = list.Item{
			ID: list.ItemID{ID: fmt.Sprintf("item-%02d", i)}, Primary: name,
			SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: name}},
		}
	}
	return list.Snapshot{Items: items}
}

func visibleIDs(m model) []list.ItemID {
	rows := m.items.VisibleRows()
	ids := make([]list.ItemID, len(rows))
	for i, row := range rows {
		ids[i] = row.Item.ID
	}
	return ids
}

func TestRunRejectsInvalidInitialProjectionBeforeStartingTea(t *testing.T) {
	invalid := list.Snapshot{Items: []list.Item{
		{ID: list.ItemID{ID: "same"}, Primary: "one"},
		{ID: list.ItemID{ID: "same"}, Primary: "two"},
	}}
	_, err := Run(context.Background(), invalid, DefaultKeyMap(), DefaultStartMode(), nil, PreviewOptions{Width: DefaultPreviewWidth}, StatusOptions{})
	if err == nil || !strings.Contains(err.Error(), "duplicate item ID") {
		t.Fatalf("Run error = %v, want duplicate item ID", err)
	}
}

func TestModelStartMode(t *testing.T) {
	require.Equal(t, StartModeFilter, DefaultStartMode())
	for _, test := range []struct {
		startMode StartMode
		wantMode  uiMode
	}{
		{startMode: StartModeNormal, wantMode: modeBrowse},
		{startMode: StartModeFilter, wantMode: modeFilter},
	} {
		m, err := newModelWithStartMode(viewSnapshot(), nil, DefaultKeyMap(), test.startMode)
		require.NoError(t, err)
		require.Equal(t, test.wantMode, m.mode)
	}

	empty, err := newModelWithStartMode(list.Snapshot{}, nil, DefaultKeyMap(), StartModeFilter)
	require.NoError(t, err)
	require.Equal(t, modeBrowse, empty.mode)

	for _, invalid := range []StartMode{"invalid", "jump"} {
		t.Run(string(invalid), func(t *testing.T) {
			_, err := newModelWithStartMode(viewSnapshot(), nil, DefaultKeyMap(), invalid)

			require.ErrorContains(t, err, "invalid start mode")
		})
	}
}

func TestViewKeepsSecondaryTextOnItsRowWithoutGlobalHelp(t *testing.T) {
	m := browseModel(newModel(viewSnapshot(), nil))
	m.width, m.height = 80, 20
	m = m.reflow()
	content := m.View().Content
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || !strings.Contains(terminal.Sanitize(lines[1]), "press / to filter") || strings.Contains(lines[1], "──") || !strings.Contains(lines[2], "──") {
		t.Fatalf("header hint should be one row with only a bottom divider: %q", lines[:min(3, len(lines))])
	}
	if strings.Contains(content, "KEYBINDINGS") || strings.Contains(content, "WORKSPACE:") || strings.Contains(content, "? help") {
		t.Fatalf("removed global help or workspace data is still rendered: %q", content)
	}
	for _, line := range lines {
		if strings.Contains(line, "editor") {
			if !strings.Contains(line, "~/code/editor") {
				t.Fatalf("secondary text wrapped off its row: %q", line)
			}
			return
		}
	}
	t.Fatal("editor row not rendered")
}

func TestViewPassesTrailingTextToTheRightColumn(t *testing.T) {
	m := browseModel(newModel(list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "pane"}, Primary: "deveditor", Secondary: "~/code", Trailing: "pi",
	}}}, nil))
	m.width, m.height = 80, 20
	m = m.reflow()
	line := terminal.Sanitize(m.viewList(m.layout()))
	require.Contains(t, line, "deveditor")
	require.Contains(t, line, "~/code")
	require.True(t, strings.HasSuffix(line, "pi  "))
	require.Equal(t, m.layout().listWidth, terminal.Width(line))
}

func TestCompactLayoutKeepsItsEdges(t *testing.T) {
	for _, width := range []int{4, 12, 24, 40} {
		style := responsiveFrameStyle(defaultTheme().appBorder, width, 8)
		require.True(t, style.GetBorderLeft(), "width %d lost its left edge", width)
		require.True(t, style.GetBorderRight(), "width %d lost its right edge", width)
	}

	short := responsiveFrameStyle(defaultTheme().appBorder, 24, 4)
	require.True(t, short.GetBorderLeft())
	require.True(t, short.GetBorderRight())
	require.False(t, short.GetBorderTop())
	require.False(t, short.GetBorderBottom())
}

func TestContextMenuOverlaySanitizesAndFitsNarrowTerminals(t *testing.T) {
	unsafe := "Open\x1b]0;owned\a\nnext"
	for _, width := range []int{1, 2, 3, 12, 24} {
		m := newModel(viewSnapshot(), nil)
		m.mode = modeMenu
		m.menu = menuState{Title: "WINDOW ACTIONS", Entries: []MenuEntry{{Action: ActionOpen, Label: unsafe}}}
		m.width, m.height = width, 8
		m = m.reflow()
		content := m.View().Content
		if strings.Contains(content, "owned") || strings.Contains(content, "\nnext") {
			t.Fatalf("width %d rendered unsafe menu text: %q", width, content)
		}
		for _, line := range strings.Split(content, "\n") {
			if actual := terminal.Width(line); actual > width {
				t.Fatalf("width %d rendered menu line width %d: %q", width, actual, line)
			}
		}
	}
}

func TestContextMenuViewportFitsAndCentersTheFullMenu(t *testing.T) {
	entries := []MenuEntry{
		{Action: ActionOpen, Label: "Open destination"},
		{Action: ActionRename, Label: "Rename window"},
		{Action: ActionRenameSession, Label: "Rename session"},
		{Action: ActionCreateWindow, Label: "New window"},
		{Action: ActionCreateSession, Label: "New session"},
		{Action: ActionDelete, Label: "Delete window"},
		{Action: ActionDeleteSession, Label: "Delete session"},
		{Action: ActionRefresh, Label: "Refresh"},
		{Action: ActionToggleProjection, Label: "Toggle projection"},
	}

	short := newModel(viewSnapshot(), nil)
	short.mode = modeMenu
	short.menu = menuState{Title: "DESTINATION ACTIONS", Entries: entries, Cursor: len(entries) - 1}
	short.width = 24
	var content string
	for height := 1; height <= 8; height++ {
		short.height = height
		short = short.reflow()
		content = short.View().Content
		if got, limit := lipgloss.Height(content), max(3, height); got > limit {
			t.Fatalf("short menu height = %d, want at most %d: %q", got, limit, content)
		}
	}
	plain := terminal.Sanitize(content)
	if !strings.Contains(plain, "‹ctrl+t› Toggle p...") || strings.Contains(plain, "Open destination") {
		t.Fatalf("menu viewport did not follow the selection: %q", plain)
	}

	centered := short
	centered.width, centered.height = 80, 20
	centered.menu.Cursor = 0
	centered = centered.reflow()
	content = centered.View().Content
	plain = terminal.Sanitize(content)
	require.Contains(t, plain, "‹ctrl+r› Refresh")
	require.Contains(t, plain, "‹ctrl+t› Toggle projection")
	overlay := renderMenu(menuProps{
		LineWidth: 80, MaxHeight: lipgloss.Height(content), Title: centered.menu.Title,
		Entries: entries, Keys: centered.keys, Theme: centered.theme,
	})
	wantX := (80 - lipgloss.Width(overlay)) / 2
	wantY := (lipgloss.Height(content) - lipgloss.Height(overlay)) / 2
	lines := strings.Split(content, "\n")
	require.Less(t, wantY, len(lines))
	line := terminal.Sanitize(lines[wantY])
	border := strings.Index(line, "╭")
	require.NotEqual(t, -1, border, "centered overlay border missing from %q", line)
	require.Equal(t, wantX, terminal.Width(line[:border]))
}

func TestViewSanitizesExternalTextAndFitsWidth(t *testing.T) {
	unsafe := "\x1b]0;owned\a中👨‍👩‍👧é\nnext"
	snapshot := list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "unsafe"}, Primary: unsafe, SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: unsafe}},
	}}, ActiveItemID: list.ItemID{ID: "unsafe"}}
	for _, width := range []int{1, 2, 3, 20} {
		m := newModel(snapshot, nil)
		m.width, m.height = width, 8
		m = m.reflow()
		content := m.View().Content
		if strings.Contains(content, "owned") || strings.Contains(content, "\nnext") {
			t.Fatalf("width %d rendered unsafe external text: %q", width, content)
		}
		for _, line := range strings.Split(content, "\n") {
			if actual := terminal.Width(line); actual > width {
				t.Fatalf("width %d rendered line width %d: %q", width, actual, line)
			}
		}
	}
}
