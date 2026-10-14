package list

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func item(id, primary, secondary string) Item {
	fields := []SearchField{{Tier: SearchPrimary, Text: primary}}
	if secondary != "" {
		fields = append(fields, SearchField{Tier: SearchSecondary, Text: secondary})
	}
	return Item{ID: ItemID{ID: id}, Primary: primary, Secondary: secondary, SearchFields: fields}
}

func flatFixture() Snapshot {
	return Snapshot{Items: []Item{
		item("editor", "editor", "~/code/editor"),
		item("server", "server", "~/svc/api"),
		item("shell", "shell", "~/prod/app"),
	}, ActiveItemID: ItemID{ID: "editor"}}
}

func nestedFixture() Snapshot {
	dev := item("dev", "dev", "")
	dev.Children = []Item{item("editor", "editor", "~/code/editor"), item("server", "server", "~/svc/api")}
	prod := item("prod", "prod", "")
	prod.Children = []Item{item("shell", "shell", "~/prod/app")}
	return Snapshot{Items: []Item{dev, prod}, ActiveItemID: ItemID{ID: "editor"}}
}

func selectedID(m Model) ItemID {
	selected, ok := m.Selected()
	if !ok {
		return ItemID{}
	}
	return selected.Item.ID
}

func rowIDs(rows []Row) []ItemID {
	ids := make([]ItemID, len(rows))
	for i, row := range rows {
		ids[i] = row.Item.ID
	}
	return ids
}

func TestFlatAndNestedListsShareMovementFilterAndViewport(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot Snapshot
		wantRows []ItemID
	}{
		{name: "flat", snapshot: flatFixture(), wantRows: []ItemID{{ID: "editor"}, {ID: "server"}, {ID: "shell"}}},
		{name: "nested", snapshot: nestedFixture(), wantRows: []ItemID{{ID: "dev"}, {ID: "editor"}, {ID: "server"}, {ID: "prod"}, {ID: "shell"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, err := New(test.snapshot)
			require.NoError(t, err)
			require.Equal(t, test.wantRows, rowIDs(m.Rows()))

			m.Resize(2)
			m.Bottom()
			require.Equal(t, test.wantRows[len(test.wantRows)-1], selectedID(m))
			require.Len(t, m.VisibleRows(), 2)
			require.Greater(t, m.Offset(), 0)
			m.Top()
			require.Equal(t, test.wantRows[0], selectedID(m))
			require.Zero(t, m.Offset())

			m.SetQuery("api")
			require.Equal(t, ItemID{ID: "server"}, selectedID(m))
			require.Contains(t, rowIDs(m.Rows()), ItemID{ID: "server"})
			if test.name == "nested" {
				require.Contains(t, rowIDs(m.Rows()), ItemID{ID: "dev"}, "a matching child must retain its ancestor")
			}
			m.SetQuery("")
			require.Equal(t, ItemID{ID: "server"}, selectedID(m))
			m.SetQuery("no-such-item")
			require.Empty(t, m.Rows())
		})
	}
}

func TestPrimarySearchTierOutranksSecondaryWithoutReorderingRows(t *testing.T) {
	snapshot := Snapshot{Items: []Item{
		item("path", "shell", "~/svc/api"),
		item("name", "api", "~/other"),
	}}
	m, err := New(snapshot)
	require.NoError(t, err)
	m.SetQuery("api")

	require.Equal(t, []ItemID{{ID: "name"}, {ID: "path"}}, rowIDs(m.Rows()), "stronger fuzzy matches must sort first")
	require.Greater(t, m.Rows()[0].Score, m.Rows()[1].Score, "primary matches must outrank secondary matches")
}

func TestFilteringSelectsTheBestMatchInsteadOfKeepingAWeakerFlatMatch(t *testing.T) {
	m, err := New(Snapshot{Items: []Item{
		item("path-match", "shell", "~/work/app"),
		item("name-match", "app", "~/other"),
	}})
	require.NoError(t, err)
	require.True(t, m.Select(ItemID{ID: "path-match"}))

	m.SetQuery("app")

	require.Equal(t, []ItemID{{ID: "name-match"}, {ID: "path-match"}}, rowIDs(m.Rows()))
	require.Equal(t, ItemID{ID: "name-match"}, selectedID(m))
}

func TestNestedFilteringRanksPrimaryTierBeforeSecondaryAndKeepsAncestors(t *testing.T) {
	secondary := item("secondary-root", "first", "")
	secondary.Children = []Item{item("secondary", "shell", "~/svc/api")}
	primary := item("primary-root", "second", "")
	primary.Children = []Item{item("primary", "api", "~/other")}
	m, err := New(Snapshot{Items: []Item{secondary, primary}})
	require.NoError(t, err)
	m.SetQuery("api")

	require.Equal(t, []ItemID{{ID: "primary-root"}, {ID: "primary"}, {ID: "secondary-root"}, {ID: "secondary"}}, rowIDs(m.Rows()))
	require.Equal(t, 0, m.Rows()[0].Score, "retained ancestors are not direct matches")
	require.Greater(t, m.Rows()[1].Score, m.Rows()[3].Score)
}

func TestEqualFilterScoresPreserveIncomingOrder(t *testing.T) {
	m, err := New(Snapshot{Items: []Item{item("first", "api", ""), item("second", "api", "")}})
	require.NoError(t, err)
	m.SetQuery("api")
	require.Equal(t, []ItemID{{ID: "first"}, {ID: "second"}}, rowIDs(m.Rows()))
}

func TestActiveItemMarksItsVisibleAncestors(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	require.True(t, m.IsActive(ItemID{ID: "dev"}))
	require.True(t, m.IsActive(ItemID{ID: "editor"}))
	require.False(t, m.IsActive(ItemID{ID: "server"}))
	require.False(t, m.IsActive(ItemID{ID: "prod"}))
}

func TestExpansionAndReplacementPreserveStableSelection(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	require.True(t, m.Select(ItemID{ID: "prod"}))
	require.True(t, m.CollapseAll())
	require.Equal(t, ItemID{ID: "prod"}, selectedID(m))
	require.True(t, m.ExpandAll())
	require.Equal(t, ItemID{ID: "prod"}, selectedID(m))

	m.expanded[ItemID{ID: "removed"}] = true
	reordered := nestedFixture()
	reordered.Items[0], reordered.Items[1] = reordered.Items[1], reordered.Items[0]
	require.NoError(t, m.Replace(reordered, ItemID{}))
	require.Equal(t, ItemID{ID: "prod"}, selectedID(m))
	require.NotContains(t, m.expanded, ItemID{ID: "removed"})
}

func TestReplacementPreservesStructuralSelectionAndExpansion(t *testing.T) {
	for _, test := range []struct {
		name  string
		other ItemID
	}{
		{name: "different scope", other: ItemID{Scope: "other", Parent: "parent", ID: "branch"}},
		{name: "different parent", other: ItemID{Scope: "scope", Parent: "other", ID: "branch"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := item("branch", "first", "")
			first.ID = ItemID{Scope: "scope", Parent: "parent", ID: "branch"}
			first.Children = []Item{item("first-child", "first child", "")}
			second := item("branch", "second", "")
			second.ID = test.other
			second.Children = []Item{item("second-child", "second child", "")}
			m, err := New(Snapshot{Items: []Item{first, second}})
			require.NoError(t, err)
			require.True(t, m.Select(second.ID))
			require.True(t, m.ToggleSelected(false))

			err = m.Replace(Snapshot{Items: []Item{second, first}}, ItemID{})

			require.NoError(t, err)
			require.Equal(t, second.ID, selectedID(m))
			require.Equal(t, []ItemID{second.ID, first.ID, first.Children[0].ID}, rowIDs(m.Rows()))
			require.False(t, m.Expansion()[second.ID])
			require.True(t, m.Expansion()[first.ID])
		})
	}
}

func TestInvalidReplacementRetainsAllListState(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	m.Resize(2)
	require.True(t, m.Select(ItemID{ID: "server"}))
	m.SetQuery("server")
	beforeRows := rowIDs(m.Rows())
	beforeOffset := m.Offset()

	invalid := nestedFixture()
	invalid.Items[1].ID = ItemID{ID: "dev"}
	require.Error(t, m.Replace(invalid, ItemID{}))
	require.Equal(t, beforeRows, rowIDs(m.Rows()))
	require.Equal(t, beforeOffset, m.Offset())
	require.Equal(t, ItemID{ID: "server"}, selectedID(m))
	require.Equal(t, "server", m.Query())
}

func TestEmptyListUsesTheSameSafePipeline(t *testing.T) {
	m, err := New(Snapshot{})
	require.NoError(t, err)
	m.Resize(3)
	m.Move(1)
	m.Page(1)
	m.SetQuery("missing")
	require.Empty(t, m.Rows())
	require.Empty(t, m.VisibleRows())
	_, ok := m.Selected()
	require.False(t, ok)
	require.Zero(t, m.Offset())
}

func TestSelectSurvivorUsesTheOldRowOrderAroundTheAnchor(t *testing.T) {
	for _, test := range []struct {
		name     string
		previous []ItemID
		anchor   ItemID
		current  Snapshot
		want     ItemID
		ok       bool
	}{
		{name: "next", previous: []ItemID{{ID: "one"}, {ID: "removed"}, {ID: "three"}}, anchor: ItemID{ID: "removed"}, current: Snapshot{Items: []Item{item("one", "one", ""), item("three", "three", "")}}, want: ItemID{ID: "three"}, ok: true},
		{name: "previous", previous: []ItemID{{ID: "one"}, {ID: "removed"}}, anchor: ItemID{ID: "removed"}, current: Snapshot{Items: []Item{item("one", "one", "")}}, want: ItemID{ID: "one"}, ok: true},
		{name: "skip other removed rows", previous: []ItemID{{ID: "one"}, {ID: "removed-a"}, {ID: "removed-b"}, {ID: "four"}}, anchor: ItemID{ID: "removed-a"}, current: Snapshot{Items: []Item{item("one", "one", ""), item("four", "four", "")}}, want: ItemID{ID: "four"}, ok: true},
		{name: "unknown anchor", previous: []ItemID{{ID: "one"}}, anchor: ItemID{ID: "removed"}, current: Snapshot{Items: []Item{item("one", "one", "")}}, want: ItemID{ID: "one"}, ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, err := New(test.current)
			require.NoError(t, err)
			require.Equal(t, test.ok, m.SelectSurvivor(test.previous, test.anchor))
			require.Equal(t, test.want, selectedID(m))
		})
	}
}

func TestMatchNavigationUsesOnlyMatchingRows(t *testing.T) {
	snapshot := Snapshot{Items: []Item{
		item("first", "api one", ""),
		item("second", "not a match", ""),
		item("third", "api two", ""),
	}}
	m, err := New(snapshot)
	require.NoError(t, err)
	m.SetQuery("api")
	require.Equal(t, ItemID{ID: "first"}, selectedID(m))
	require.True(t, m.JumpMatch(true))
	require.Equal(t, ItemID{ID: "third"}, selectedID(m))
	current, total := m.MatchPosition()
	require.Equal(t, 2, current)
	require.Equal(t, 2, total)
}
