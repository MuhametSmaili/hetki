package tui

import (
	"testing"

	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestHeaderShowsNonFatalSnapshotNotice(t *testing.T) {
	m := newModel(list.Snapshot{Notice: "frecency state is corrupt"}, nil)
	require.Equal(t, "frecency state is corrupt", headerRight(m))
	m.busy = true
	m.status = "refreshing..."
	require.Equal(t, "refreshing...", headerRight(m))
}

func TestHeaderTracksMode(t *testing.T) {
	m := model{keys: DefaultKeyMap()}
	for _, tt := range []struct {
		mode       uiMode
		wantPrompt string
		wantHint   string
	}{
		{mode: modeBrowse, wantPrompt: " NORMAL ", wantHint: "press / to filter or ; to jump"},
		{mode: modeFilter, wantPrompt: " FILTER "},
		{mode: modeJump, wantPrompt: " JUMP ", wantHint: "type a label to jump or / to filter"},
		{mode: modeMenu},
	} {
		m.mode = tt.mode
		require.Equal(t, tt.wantPrompt, headerPrompt(m))
		require.Equal(t, tt.wantHint, headerHint(m))
	}

	m = newModel(viewSnapshot(), nil)
	m.mode = modeFilter
	m.items.SetQuery("dev")
	require.Empty(t, headerHint(m))
}

func TestHeaderHintsFollowInjectedKeymap(t *testing.T) {
	keys, err := ResolveKeyMap(map[KeyMode][]Binding{
		KeyModeNormal: {
			{Action: ActionFilter, Keys: []string{"ctrl+f"}},
			{Action: ActionJump, Keys: []string{"ctrl+j"}},
		},
		KeyModeJump: {
			{Action: ActionFilter, Keys: []string{"ctrl+f"}},
			{Action: ActionJump, Keys: []string{"ctrl+j"}}, // Unhandled here: must not become a hint.
		},
	})
	require.NoError(t, err)
	m, err := newModelWithKeys(viewSnapshot(), nil, keys)
	require.NoError(t, err)
	m = browseModel(m)
	m.width, m.height = 120, 20
	m = m.reflow()
	require.Equal(t, "press ctrl+f to filter or ctrl+j to jump", headerHint(m))
	require.Contains(t, m.View().Content, headerHint(m))

	m, _ = updateModel(t, m, controlKey('j'))
	require.Equal(t, modeJump, m.mode)
	require.Equal(t, "type a label to jump or ctrl+f to filter", headerHint(m))
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
