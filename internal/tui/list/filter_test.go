package list

import (
	"testing"

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
				require.Equal(t, itemScore(row.Item, test.query), row.Score)
			}

			model.SetQuery("")

			require.Equal(t, []Item{branch, strong, item("tie", "api", "")}, model.Snapshot().Items[0].Children)
			require.Equal(t, []Item{item("hidden", "shell", "")}, model.Snapshot().Items[0].Children[1].Children)
			require.Equal(t, []ItemID{{ID: "root"}, {ID: "other"}}, rowIDs(model.Rows()))
		})
	}
}
