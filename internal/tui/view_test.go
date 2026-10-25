package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestHeaderShowsNonFatalSnapshotNotice(t *testing.T) {
	m := newModel(list.Snapshot{Notice: "frecency state is corrupt"}, nil)
	require.Equal(t, "frecency state is corrupt", headerRight(m))
	m.busy = true
	m.status = "refreshing..."
	require.Equal(t, "refreshing...", headerRight(m))
}

func TestHeaderKeepsLeadingWarningVisible(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{"disabled", false},
		{"enabled", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(list.Snapshot{Notice: "configuration warning: ui.view uses default flat\n" + strings.Repeat("frecency unavailable: long path/", 20)}, nil)
			m.width, m.height, m.preview.enabled = 120, 24, tc.enabled
			m = m.reflow()

			content := terminal.Sanitize(m.View().Content)

			require.Contains(t, content, "configuration warning")
			for _, line := range strings.Split(m.View().Content, "\n") {
				require.LessOrEqual(t, terminal.Width(line), m.width)
			}
		})
	}
}

func TestHeaderTracksMode(t *testing.T) {
	m := model{keys: DefaultKeyMap()}
	for _, tt := range []struct {
		mode       uiMode
		wantPrompt string
		wantHint   string
	}{
		{mode: modeBrowse, wantPrompt: ">", wantHint: "press / to filter"},
		{mode: modeFilter, wantPrompt: ">"},
		{mode: modeMenu},
	} {
		m.mode = tt.mode
		require.Equal(t, tt.wantPrompt, headerPrompt(m))
		require.Equal(t, tt.wantHint, headerHint(m))
	}

	m = newModel(viewSnapshot(), nil)
	m.mode = modeBrowse
	m.items.SetQuery("dev")
	require.Empty(t, headerHint(m))
	require.Equal(t, ">", headerPrompt(m))
}

func TestBrowseHeaderShowsHintOnlyWithoutQuery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		previewWidth int
		query        string
		wantPrefix   string
	}{
		{name: "full-width empty", wantPrefix: "> press / to filter"},
		{name: "preview empty", previewWidth: 20, wantPrefix: "> press / to filter"},
		{name: "full-width retained query", query: "editor", wantPrefix: "> editor"},
		{name: "preview retained query", previewWidth: 20, query: "editor", wantPrefix: "> editor"},
		{name: "whitespace query", query: "  ", wantPrefix: "> press / to filter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := browseModel(newModel(viewSnapshot(), nil))
			m.items.SetQuery(tc.query)
			layout := layoutMetrics{listWidth: 80, previewWidth: tc.previewWidth, headerHeight: 3}

			header := m.viewHeader(layout)

			first := strings.Split(header, "\n")[0]
			plain := strings.TrimSpace(terminal.Sanitize(first))
			require.True(t, strings.HasPrefix(plain, tc.wantPrefix))
			require.NotContains(t, plain, "NORMAL")
			if strings.TrimSpace(tc.query) != "" {
				require.NotContains(t, plain, "press ")
			}
		})
	}
}

func TestLeavingFilterModeKeepsNarrowQueryWithoutHint(t *testing.T) {
	for _, key := range []string{"/", "ctrl+f"} {
		t.Run(key, func(t *testing.T) {
			keys, err := ResolveKeyMap(map[KeyMode][]Binding{
				KeyModeNormal: {{Action: ActionFilter, Keys: []string{key}}},
				KeyModeFilter: {{Action: ActionCancel, Keys: []string{"esc"}}},
			})
			require.NoError(t, err)
			m, err := newModelWithKeys(viewSnapshot(), nil, keys)
			require.NoError(t, err)
			m.width, m.height, m.preview.enabled = 48, 12, true
			m.items.SetQuery("editor")
			m = m.reflow()

			m, _ = updateModel(t, m, specialKey(tea.KeyEscape))

			require.Equal(t, modeBrowse, m.mode)
			require.Equal(t, "editor", m.items.Query())
			require.Equal(t, 20, m.layout().listWidth)
			first := strings.Split(m.viewHeader(m.layout()), "\n")[0]
			require.Equal(t, "> editor", strings.TrimSpace(terminal.Sanitize(first)))
		})
	}
}

func TestFilterHeaderUsesDedicatedPromptStyle(t *testing.T) {
	for _, tc := range []struct {
		name         string
		previewWidth int
	}{
		{name: "full-width"},
		{name: "preview", previewWidth: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(viewSnapshot(), nil)
			m.mode = modeFilter
			m.theme.filterPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
			m.theme.activeRow = lipgloss.NewStyle().Background(lipgloss.Color("1"))
			layout := layoutMetrics{listWidth: 80, previewWidth: tc.previewWidth}

			header := m.viewHeader(layout)

			require.True(t, strings.HasPrefix(header, m.theme.filterPrompt.Render(">")))
		})
	}
}

func TestHeaderHintsFollowInjectedKeymap(t *testing.T) {
	keys, err := ResolveKeyMap(map[KeyMode][]Binding{
		KeyModeNormal: {
			{Action: ActionFilter, Keys: []string{"ctrl+f"}},
		},
	})
	require.NoError(t, err)
	m, err := newModelWithKeys(viewSnapshot(), nil, keys)
	require.NoError(t, err)
	m = browseModel(m)
	m.width, m.height = 120, 20
	m = m.reflow()
	require.Equal(t, "press ctrl+f to filter", headerHint(m))
	require.Contains(t, m.View().Content, headerHint(m))

	m, _ = updateModel(t, m, controlKey('f'))
	require.Equal(t, modeFilter, m.mode)
	require.Empty(t, headerHint(m))

	m.keys = KeyMap{}
	m.mode = modeBrowse
	require.Empty(t, headerHint(m), "do not advertise an unbound action")
}

func TestRootCountLabelShowsFilteredRootsOverTotal(t *testing.T) {
	snapshot := list.Snapshot{Items: []list.Item{
		{
			ID: list.ItemID{ID: "session-1"}, Primary: "dev", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "dev"}},
			Children: []list.Item{{ID: list.ItemID{ID: "window-1"}, Primary: "editor", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "editor"}}}},
		},
		{
			ID: list.ItemID{ID: "session-2"}, Primary: "prod", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "prod"}},
			Children: []list.Item{{ID: list.ItemID{ID: "window-2"}, Primary: "logs", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "logs"}}}},
		},
	}}
	model, err := list.New(snapshot)
	require.NoError(t, err)
	require.Equal(t, "2", rootCountLabel(snapshot, model.ShownRoots(), model.Query()))

	model.SetQuery("logs")
	require.Equal(t, "1/2", rootCountLabel(snapshot, model.ShownRoots(), model.Query()))
	model.SetQuery("missing")
	require.Equal(t, "0/2", rootCountLabel(snapshot, model.ShownRoots(), model.Query()))
}
