package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/frecency"
	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectFlatBuildsStablePaneDestinations(t *testing.T) {
	state := backend.StateResult{
		Sessions: []backend.Session{{
			ID: "$1", Name: "dev", Windows: []backend.Window{
				{ID: "@1", Name: "editor", Index: 1, Active: true, Panes: []backend.Pane{
					{ID: "%5", Index: 5, Path: "/home/me/code", Command: "node"},
					{ID: "%7", Index: 7, Path: "/home/me/code", Command: "node", Program: "pi", Active: true},
					{ID: "%2", Index: 2, Path: "/var/log"},
				}},
				{ID: "@2", Name: " \t ", Index: 2, Panes: []backend.Pane{{ID: "%8", Index: 0, Path: "/home/me/code", Active: true}}},
			},
		}},
		Active: backend.ActiveContext{SessionID: "$1", WindowID: "@1", PaneID: "%7"},
	}

	snapshot, index, err := projectFlat(state, "/home/me")
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 4)

	codeID := destinationItemID("$1", "@1", "%7")
	code := index[codeID]
	assert.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%7"}, code.Target)

	assert.Equal(t, "dev", itemByID(snapshot.Items, codeID).Context)
	assert.Equal(t, "editor", itemByID(snapshot.Items, codeID).Primary)
	assert.Equal(t, "2", itemByID(snapshot.Items, destinationItemID("$1", "@2", "%8")).Primary)
	assert.Equal(t, "·7", itemByID(snapshot.Items, codeID).Annotation)
	assert.Empty(t, itemByID(snapshot.Items, destinationItemID("$1", "@2", "%8")).Annotation)
	assert.Equal(t, "pi", itemByID(snapshot.Items, codeID).Trailing)
	assert.Equal(t, "node", itemByID(snapshot.Items, destinationItemID("$1", "@1", "%5")).Trailing)
	assert.Equal(t, "~/code", itemByID(snapshot.Items, codeID).Secondary)
	assert.Equal(t, codeID, snapshot.ActiveItemID)
	assert.NotEqual(t, codeID, destinationItemID("$1", "@2", "%7"))

	fields := itemByID(snapshot.Items, codeID).SearchFields
	assert.Equal(t, []list.SearchField{
		{Tier: list.SearchPrimary, Text: "dev"},
		{Tier: list.SearchPrimary, Text: "editor"},
		{Tier: list.SearchPrimary, Text: "node"},
		{Tier: list.SearchPrimary, Text: "pi"},
		{Tier: list.SearchSecondary, Text: "/home/me/code"},
		{Tier: list.SearchSecondary, Text: "~/code"},
	}, fields)
}

func TestProjectFlatKeepsDuplicateAndDelimitedContextsDistinct(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{
		{ID: "$1", Name: "same > name\n", Windows: []backend.Window{
			{ID: "@1", Name: "same:window", Panes: []backend.Pane{{ID: "%1", Path: "/work/a:b\tctx"}}},
			{ID: "@3", Name: "same:window", Panes: []backend.Pane{{ID: "%3", Path: "/work/a:b\tctx"}}},
		}},
		{ID: "$2", Name: "same > name\n", Windows: []backend.Window{{ID: "@2", Name: "same:window", Panes: []backend.Pane{{ID: "%2", Path: "/work/a:b\tctx"}}}}},
	}}

	snapshot, index, err := projectFlat(state, "")
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 3)
	assert.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"}, index[destinationItemID("$1", "@1", "%1")].Target)
	assert.Equal(t, backend.Target{SessionID: "$1", WindowID: "@3", PaneID: "%3"}, index[destinationItemID("$1", "@3", "%3")].Target)
	assert.Equal(t, backend.Target{SessionID: "$2", WindowID: "@2", PaneID: "%2"}, index[destinationItemID("$2", "@2", "%2")].Target)
}

func TestProjectFlatAllowsWindowsWithoutPanes(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{{
		ID: "$1", Name: "empty", Windows: []backend.Window{{ID: "@1", Name: "empty"}},
	}}}

	snapshot, index, err := projectFlat(state, "")
	require.NoError(t, err)
	assert.Empty(t, snapshot.Items)
	assert.Empty(t, index)
}

func TestProjectFlatKeepsEveryPaneWithoutAnActivePane(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{{
		ID: "$1", Name: "dev", Windows: []backend.Window{{
			ID: "@1", Name: "editor", Panes: []backend.Pane{
				{ID: "%9", Index: 9, Path: "/code"},
				{ID: "%2", Index: 2, Path: "/code"},
			},
		}},
	}}}

	snapshot, index, err := projectFlat(state, "")
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 2)
	assert.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%2"}, index[destinationItemID("$1", "@1", "%2")].Target)
	assert.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%9"}, index[destinationItemID("$1", "@1", "%9")].Target)
	assert.Equal(t, destinationItemID("$1", "@1", "%2"), snapshot.Items[0].ID)
}

func TestProjectFlatKeepsPanesWithEquivalentDisplayPathsDistinct(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{{
		ID: "$1", Name: "dev", Windows: []backend.Window{{
			ID: "@1", Name: "editor", Panes: []backend.Pane{
				{ID: "%1", Index: 0, Path: "/home/me/code"},
				{ID: "%2", Index: 1, Path: "~/code"},
			},
		}},
	}}}

	snapshot, _, err := projectFlat(state, "/home/me")
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 2)
	assert.Equal(t, snapshot.Items[0].Secondary, snapshot.Items[1].Secondary)
	assert.NotEqual(t, snapshot.Items[0].ID, snapshot.Items[1].ID)
}

func TestFlatProjectionSearchesSessionWindowAndPathWithNamePriority(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{
		{ID: "$1", Name: "needle", Windows: []backend.Window{{ID: "@1", Name: "editor", Panes: []backend.Pane{{ID: "%1", Path: "/work/code"}}}}},
		{ID: "$2", Name: "other", Windows: []backend.Window{{ID: "@2", Name: "logs", Panes: []backend.Pane{{ID: "%2", Path: "/work/needle"}}}}},
	}}
	snapshot, _, err := projectFlat(state, "")
	require.NoError(t, err)
	model, err := list.New(snapshot)
	require.NoError(t, err)

	model.SetQuery("needle")
	require.Len(t, model.Rows(), 2)
	assert.Equal(t, destinationItemID("$1", "@1", "%1"), model.Rows()[0].Item.ID)
	model.SetQuery("editor")
	require.Len(t, model.Rows(), 1)
	model.SetQuery("work/needle")
	require.Len(t, model.Rows(), 1)
	assert.Equal(t, destinationItemID("$2", "@2", "%2"), model.Rows()[0].Item.ID)
}

func TestProjectFlatRejectsMissingPaneIDs(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{{
		ID: "$1", Name: "dev", Windows: []backend.Window{{
			ID: "@1", Name: "editor", Panes: []backend.Pane{{ID: "", Path: "/code"}},
		}},
	}}}

	_, _, err := projectFlat(state, "")
	require.ErrorContains(t, err, "pane is missing its ID")
}

func TestProjectFlatIdentitySurvivesPathCommandAndIndexChanges(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{{
		ID: "$1", Name: "dev", Windows: []backend.Window{{
			ID: "@1", Name: "editor", Panes: []backend.Pane{{ID: "%1", Command: "node", Path: "/before"}},
		}},
	}}}
	before, beforeIndex, err := projectFlat(state, "")
	require.NoError(t, err)
	pane := &state.Sessions[0].Windows[0].Panes[0]
	pane.Command, pane.Program, pane.Path, pane.Index = "node", "pi", "/after", 4
	after, afterIndex, err := projectFlat(state, "")
	require.NoError(t, err)
	require.Equal(t, before.Items[0].ID, after.Items[0].ID)
	require.Equal(t, beforeIndex[before.Items[0].ID].Target, afterIndex[after.Items[0].ID].Target)
	model, err := list.New(after)
	require.NoError(t, err)
	for _, query := range []string{"pi", "node", "after"} {
		model.SetQuery(query)
		require.Len(t, model.Rows(), 1, query)
	}
	pane.Dead = true
	dead, _, err := projectFlat(state, "")
	require.NoError(t, err)
	require.Equal(t, "exited", dead.Items[0].Trailing)
}

func TestProjectFlatKeepsLinkedPaneOccurrencesDistinct(t *testing.T) {
	window := backend.Window{ID: "@1", Name: "shared", Panes: []backend.Pane{{ID: "%1", Path: "/same", Command: "node"}}}
	state := backend.StateResult{
		Sessions: []backend.Session{{ID: "$1", Name: "first", Windows: []backend.Window{window}}, {ID: "$2", Name: "second", Windows: []backend.Window{window}}},
		Active:   backend.ActiveContext{SessionID: "$2", WindowID: "@1", PaneID: "%1"},
	}
	snapshot, index, err := projectFlat(state, "")
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 2)
	require.Equal(t, destinationItemID("$2", "@1", "%1"), snapshot.ActiveItemID)
	require.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"}, index[destinationItemID("$1", "@1", "%1")].Target)
	require.Equal(t, backend.Target{SessionID: "$2", WindowID: "@1", PaneID: "%1"}, index[destinationItemID("$2", "@1", "%1")].Target)
}

func TestProjectFlatMatchesTheCompleteActiveOccurrence(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{{
		ID: "session", Name: "session", Windows: []backend.Window{
			{ID: "first", Panes: []backend.Pane{{ID: "shared"}}},
			{ID: "second", Panes: []backend.Pane{{ID: "shared"}}},
		},
	}}}
	for _, test := range []struct {
		name   string
		active backend.ActiveContext
		want   list.ItemID
	}{
		{name: "first window", active: backend.ActiveContext{SessionID: "session", WindowID: "first", PaneID: "shared"}, want: destinationItemID("session", "first", "shared")},
		{name: "second window", active: backend.ActiveContext{SessionID: "session", WindowID: "second", PaneID: "shared"}, want: destinationItemID("session", "second", "shared")},
		{name: "missing window", active: backend.ActiveContext{SessionID: "session", PaneID: "shared"}},
		{name: "unknown window", active: backend.ActiveContext{SessionID: "session", WindowID: "unknown", PaneID: "shared"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state.Active = test.active

			snapshot, _, err := projectFlat(state, "")

			require.NoError(t, err)
			require.Equal(t, test.want, snapshot.ActiveItemID)
		})
	}
}

func TestProjectFlatLabelsEveryPaneInSplitWindows(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{{
		ID: "$1", Name: "dev", Windows: []backend.Window{{
			ID: "@1", Name: "editor", Panes: []backend.Pane{
				{ID: "%1", Index: 0, Path: "/code", Command: "node", Program: "pi", Active: true},
				{ID: "%2", Index: 1, Path: "/code", Command: "node", Program: "pi"},
				{ID: "%3", Index: 2, Path: "/code", Command: "nvim"},
				{ID: "%4", Index: 3, Path: "/other", Command: "node", Program: "pi"},
			},
		}},
	}}}

	snapshot, _, err := projectFlat(state, "")

	require.NoError(t, err)
	require.Len(t, snapshot.Items, 4)
	for i, paneID := range []string{"%1", "%2", "%3", "%4"} {
		item := itemByID(snapshot.Items, destinationItemID("$1", "@1", paneID))
		require.Equal(t, "dev", item.Context)
		require.Equal(t, "editor", item.Primary)
		require.Equal(t, fmt.Sprintf("·%d", i), item.Annotation)
	}

	model, err := list.New(snapshot)
	require.NoError(t, err)

	model.SetQuery("nvim")

	require.Len(t, model.Rows(), 1)
	require.Equal(t, "·2", model.Rows()[0].Item.Annotation)
}

func itemByID(items []list.Item, id list.ItemID) list.Item {
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	return list.Item{}
}

func TestProjectFlatUsesTotalFrecencyOrderAndFilteredTieBreaks(t *testing.T) {
	now := time.Unix(2_000_000, 0)
	scores := frecency.NewScores([]frecency.Record{
		{Path: "/hot", Session: "old", Rank: 10, LastUsed: now.Unix()},
		{Path: "/cold", Session: "alpha", Rank: 8, LastUsed: now.Unix()},
		{Path: "/shared", Session: "alpha", Rank: 1, LastUsed: now.Unix()},
		{Path: "/shared", Session: "beta", Rank: 2, LastUsed: now.Unix()},
	}, now)
	state := backend.StateResult{Sessions: []backend.Session{
		{ID: "$1", Name: "zeta", Windows: []backend.Window{{ID: "@1", Name: "hot", Index: 5, Panes: []backend.Pane{{ID: "%1", Path: "/hot"}}}}},
		{ID: "$2", Name: "alpha", Windows: []backend.Window{
			{ID: "@2", Name: "cold", Index: 1, Panes: []backend.Pane{{ID: "%2", Path: "/cold"}}},
			{ID: "@3", Name: "shared", Index: 2, Panes: []backend.Pane{{ID: "%3", Path: "/shared"}}},
		}},
		{ID: "$3", Name: "beta", Windows: []backend.Window{{ID: "@4", Name: "shared", Index: 0, Panes: []backend.Pane{{ID: "%4", Path: "/shared"}}}}},
	}}

	snapshot, _, err := projectFlatRanked(state, "", scores)
	require.NoError(t, err)
	assert.Equal(t, []list.ItemID{
		destinationItemID("$1", "@1", "%1"),
		destinationItemID("$2", "@2", "%2"),
		destinationItemID("$3", "@4", "%4"),
		destinationItemID("$2", "@3", "%3"),
	}, itemIDs(snapshot.Items))

	model, err := list.New(snapshot)
	require.NoError(t, err)
	model.SetQuery("shared")
	require.Len(t, model.Rows(), 2)
	assert.Equal(t, destinationItemID("$3", "@4", "%4"), model.Rows()[0].Item.ID)
}

func TestDestinationOrderHasDeterministicFallbacks(t *testing.T) {
	base := liveItem{SessionName: "same", WindowIndex: 1, RawPath: "/same", Target: backend.Target{SessionID: "$1", WindowID: "@1"}}
	for _, test := range []struct {
		name  string
		left  liveItem
		right liveItem
	}{
		{name: "session name", left: liveItem{SessionName: "a"}, right: liveItem{SessionName: "b"}},
		{name: "window index", left: liveItem{WindowIndex: 1}, right: liveItem{WindowIndex: 2}},
		{name: "raw path", left: liveItem{RawPath: "/a"}, right: liveItem{RawPath: "/b"}},
		{name: "session ID", left: liveItem{Target: backend.Target{SessionID: "$1"}}, right: liveItem{Target: backend.Target{SessionID: "$2"}}},
		{name: "window ID", left: liveItem{Target: backend.Target{WindowID: "@1"}}, right: liveItem{Target: backend.Target{WindowID: "@2"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			left, right := base, base
			if test.left.SessionName != "" {
				left.SessionName, right.SessionName = test.left.SessionName, test.right.SessionName
			} else if test.left.WindowIndex != 0 {
				left.WindowIndex, right.WindowIndex = test.left.WindowIndex, test.right.WindowIndex
			} else if test.left.RawPath != "" {
				left.RawPath, right.RawPath = test.left.RawPath, test.right.RawPath
			} else if test.left.Target.SessionID != "" {
				left.Target.SessionID, right.Target.SessionID = test.left.Target.SessionID, test.right.Target.SessionID
			} else {
				left.Target.WindowID, right.Target.WindowID = test.left.Target.WindowID, test.right.Target.WindowID
			}
			assert.True(t, destinationLess(left, right, frecency.Scores{}))
			assert.False(t, destinationLess(right, left, frecency.Scores{}))
		})
	}
}

func itemIDs(items []list.Item) []list.ItemID {
	ids := make([]list.ItemID, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func BenchmarkProjectFlatRankTenThousandPanes(b *testing.B) {
	state := backend.StateResult{}
	records := make([]frecency.Record, 0, 10_000)
	now := time.Unix(2_000_000, 0)
	windowID, paneID := 0, 0
	for sessionIndex := range 100 {
		session := backend.Session{ID: fmt.Sprintf("$%d", sessionIndex), Name: fmt.Sprintf("session-%d", sessionIndex)}
		for windowIndex := range 20 {
			windowID++
			window := backend.Window{ID: fmt.Sprintf("@%d", windowID), Name: fmt.Sprintf("window-%d", windowIndex), Index: windowIndex}
			for paneIndex := range 5 {
				paneID++
				path := fmt.Sprintf("/work/%d/%d/%d", sessionIndex, windowIndex, paneIndex)
				window.Panes = append(window.Panes, backend.Pane{ID: fmt.Sprintf("%%%d", paneID), Index: paneIndex, Path: path})
				records = append(records, frecency.Record{
					Path: path, Session: session.Name, Rank: float64(paneID%10 + 1), LastUsed: now.Unix(),
				})
			}
			session.Windows = append(session.Windows, window)
		}
		state.Sessions = append(state.Sessions, session)
	}

	scores := frecency.NewScores(records, now)
	b.ReportAllocs()
	for b.Loop() {
		_, _, err := projectFlatRanked(state, "/home/me", scores)
		if err != nil {
			b.Fatal(err)
		}
	}
}
