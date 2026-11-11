package list

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sahilm/fuzzy"
	"github.com/stretchr/testify/require"
)

func TestItemFilterRetainsAncestorsAndCollapsedState(t *testing.T) {
	snapshot := nestedFixture()
	snapshot.ActiveItemID = ItemID{}
	m, err := New(snapshot)
	require.NoError(t, err)
	m.CollapseAll()
	included := map[ItemID]struct{}{{ID: "server"}: {}}

	m.SetFilter("server", included)
	delete(included, ItemID{ID: "server"})
	err = m.Replace(m.Snapshot(), ItemID{ID: "server"})
	filtered := rowIDs(m.Rows())
	m.SetFilter("", nil)

	require.NoError(t, err)
	require.Equal(t, []ItemID{{ID: "dev"}, {ID: "server"}}, filtered)
	require.Equal(t, []ItemID{{ID: "dev"}, {ID: "prod"}}, rowIDs(m.Rows()))
	require.Len(t, m.Snapshot().Items[0].Children, 2)
}

func TestItemFilterDistinguishesEmptySetFromUnrestricted(t *testing.T) {
	m, err := New(flatFixture())
	require.NoError(t, err)

	m.SetFilter("", map[ItemID]struct{}{})
	empty := rowIDs(m.Rows())
	m.SetFilter("", nil)

	require.Empty(t, empty)
	require.Equal(t, []ItemID{{ID: "editor"}, {ID: "server"}, {ID: "shell"}}, rowIDs(m.Rows()))
}

func TestFilteredTreeKeepsRankedPrefixesAndBranchIdentity(t *testing.T) {
	leaf := item("leaf", "api server", "/work/target")
	branch := item("branch", "tools", "")
	branch.Children = []Item{leaf}
	strong := item("strong", "api", "")
	strong.Children = []Item{item("hidden", "shell", "")}
	root := item("root", "development", "")
	root.Children = []Item{branch, strong, item("tie", "api", "")}
	snapshot := Snapshot{Items: []Item{root, item("other", "api", "")}}

	for _, test := range []struct {
		name, query string
		ids         []ItemID
		prefixes    []string
		depths      []int
	}{
		{
			name: "ranked siblings and retained ancestors", query: "api",
			ids:      []ItemID{{ID: "root"}, {ID: "strong"}, {ID: "tie"}, {ID: "branch"}, {ID: "leaf"}, {ID: "other"}},
			prefixes: []string{"", "├─ ", "├─ ", "└─ ", "   └─ ", ""},
			depths:   []int{0, 1, 1, 1, 2, 0},
		},
		{
			name: "only a secondary grandchild matches", query: "target",
			ids:      []ItemID{{ID: "root"}, {ID: "branch"}, {ID: "leaf"}},
			prefixes: []string{"", "└─ ", "   └─ "},
			depths:   []int{0, 1, 2},
		},
		{name: "no matches", query: "missing", ids: []ItemID{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, err := New(snapshot)
			require.NoError(t, err)
			model.CollapseAll()

			model.SetQuery(test.query)

			require.Equal(t, test.ids, rowIDs(model.Rows()))
			for i, row := range model.Rows() {
				require.Equal(t, test.prefixes[i], row.TreePrefix)
				require.Equal(t, test.depths[i], row.Depth)
				require.True(t, row.Expanded)
				require.Equal(t, len(row.Item.Children) > 0, row.Branch)
				require.Equal(t, referenceItemScore(row.Item, test.query), row.Score)
			}

			model.SetQuery("")

			require.Equal(t, []Item{branch, strong, item("tie", "api", "")}, model.Snapshot().Items[0].Children)
			require.Equal(t, []Item{item("hidden", "shell", "")}, model.Snapshot().Items[0].Children[1].Children)
			require.Equal(t, []ItemID{{ID: "root"}, {ID: "other"}}, rowIDs(model.Rows()))
		})
	}
}

func TestFilteringScoresMatchIndependentFieldsAcrossBatchBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 63, 64, 65, 129} {
		t.Run(fmt.Sprintf("%d fields", count), func(t *testing.T) {
			candidate := scoringFixture(count)
			for _, query := range []string{"api", "aaa", "é", "σ", "東京", "missing", strings.Repeat("ab", 64)} {
				assertFilteredScore(t, candidate, query)
			}
		})
	}
}

func scoringFixture(count int) Item {
	texts := []string{"api", "ApiServer", "api/api", "aaaa", "école Straße Σσς", "東京", "hidden\x00api", "\xffapi", strings.Repeat("ab", 128)}
	candidate := Item{ID: ItemID{ID: "candidate"}, Primary: "candidate"}
	for i := range count {
		tier := SearchPrimary
		if count > 1 && i >= count/2 {
			tier = SearchSecondary
		}
		candidate.SearchFields = append(candidate.SearchFields, SearchField{Tier: tier, Text: texts[i%len(texts)]})
	}
	return candidate
}

func assertFilteredScore(t *testing.T, candidate Item, query string) {
	t.Helper()
	model, err := New(Snapshot{Items: []Item{candidate}})
	require.NoError(t, err)
	expected := referenceItemScore(candidate, query)

	model.SetQuery(query)

	if expected == 0 {
		require.Empty(t, model.Rows(), "query %q", query)
		return
	}

	require.Len(t, model.Rows(), 1, "query %q", query)
	require.Equal(t, expected, model.Rows()[0].Score, "query %q", query)
}

func TestFilteringRanksAndRefreshesItemsAcrossBatches(t *testing.T) {
	children := make([]Item, 130)
	for i := range children {
		children[i] = item(fmt.Sprint(i), "api server", "")
	}
	children[0].SearchFields = []SearchField{{Tier: SearchSecondary, Text: "api"}}
	children[129] = item("129", "api", "")
	first := Item{ID: ItemID{ID: "first"}, Primary: "first", Children: children[:65]}
	second := Item{ID: ItemID{ID: "second"}, Primary: "second", Children: children[65:]}
	model, err := New(Snapshot{Items: []Item{first, second}})
	require.NoError(t, err)
	model.Resize(4)
	model.CollapseAll()

	model.SetQuery("api")

	require.Len(t, model.Rows(), 132)
	require.Equal(t, second.ID, model.Rows()[0].Item.ID)
	require.Equal(t, children[129].ID, model.Rows()[1].Item.ID)
	require.Equal(t, children[0].ID, model.Rows()[131].Item.ID)
	for _, row := range model.Rows() {
		require.Equal(t, referenceItemScore(row.Item, "api"), row.Score)
	}
	require.True(t, model.Select(children[64].ID))

	model.SetDetails(map[ItemID]Details{children[0].ID: {
		SearchFields: []SearchField{{Tier: SearchPrimary, Text: "api"}},
	}})

	require.Equal(t, first.ID, model.Rows()[0].Item.ID)
	require.Equal(t, children[0].ID, model.Rows()[1].Item.ID)
	require.Equal(t, children[64].ID, selectedID(model))

	model.SetFilter("api", map[ItemID]struct{}{children[64].ID: {}, children[129].ID: {}})

	require.Equal(t, []ItemID{second.ID, children[129].ID, first.ID, children[64].ID}, rowIDs(model.Rows()))
	require.Equal(t, children[64].ID, selectedID(model))

	model.SetFilter("", nil)

	require.Equal(t, []ItemID{first.ID, second.ID}, rowIDs(model.Rows()))
	require.False(t, model.Expansion()[first.ID])
	require.False(t, model.Expansion()[second.ID])
}

func referenceItemScore(item Item, query string) int {
	best := 0
	for _, field := range item.SearchFields {
		matches := fuzzy.FindNoSort(query, []string{field.Text})
		if len(matches) == 0 {
			continue
		}

		quality := min(max(matches[0].Score, 0), maxQuality)
		priority := (int(SearchSecondary) - int(field.Tier) + 1) * 1000
		best = max(best, priority+quality)
	}
	return best
}
