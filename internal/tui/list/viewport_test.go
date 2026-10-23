package list

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func treeRowHeight(row Row) int {
	if row.Depth > 0 {
		return 2
	}
	return 1
}

func TestRowSizesStayWithinTheViewport(t *testing.T) {
	for _, tc := range []struct {
		name            string
		height, visible int
	}{
		{name: "zero", height: 0, visible: 3},
		{name: "negative", height: -2, visible: 3},
		{name: "oversized", height: 100, visible: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := New(flatFixture())
			require.NoError(t, err)

			m.ResizeRows(3, func(Row) int { return tc.height })
			m.Bottom()

			require.Len(t, m.VisibleRows(), tc.visible)
			require.Contains(t, rowIDs(m.VisibleRows()), selectedID(m))
		})
	}
}

func TestMixedHeightRowsPageAndScrollAsWholeItems(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)

	m.ResizeRows(5, treeRowHeight)

	require.Equal(t, []ItemID{{ID: "dev"}, {ID: "editor"}, {ID: "server"}}, rowIDs(m.VisibleRows()))
	m.Page(1)
	require.Equal(t, ItemID{ID: "prod"}, selectedID(m))
	require.Contains(t, rowIDs(m.VisibleRows()), selectedID(m))
	m.Page(-1)
	require.Equal(t, ItemID{ID: "dev"}, selectedID(m))
	m.Bottom()
	require.Equal(t, []ItemID{{ID: "server"}, {ID: "prod"}, {ID: "shell"}}, rowIDs(m.VisibleRows()))

	m.ResizeRows(3, treeRowHeight)
	require.Equal(t, []ItemID{{ID: "prod"}, {ID: "shell"}}, rowIDs(m.VisibleRows()))
	m.Resize(3)
	require.Equal(t, []ItemID{{ID: "server"}, {ID: "prod"}, {ID: "shell"}}, rowIDs(m.VisibleRows()))
}

func TestMixedHeightViewportReflowsAfterFilteringAndExpansion(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	m.ResizeRows(3, treeRowHeight)
	m.Bottom()

	m.SetQuery("api")

	require.Equal(t, []ItemID{{ID: "dev"}, {ID: "server"}}, rowIDs(m.VisibleRows()))
	require.Equal(t, ItemID{ID: "server"}, selectedID(m))
	m.SetQuery("")
	require.True(t, m.CollapseAll())
	require.Equal(t, []ItemID{{ID: "dev"}, {ID: "prod"}}, rowIDs(m.VisibleRows()))
	require.True(t, m.ExpandAll())
	require.Contains(t, rowIDs(m.VisibleRows()), selectedID(m))
	require.NoError(t, m.Replace(nestedFixture(), ItemID{ID: "shell"}))
	require.Equal(t, []ItemID{{ID: "prod"}, {ID: "shell"}}, rowIDs(m.VisibleRows()))
	m.ResizeRows(0, treeRowHeight)
	require.Empty(t, m.VisibleRows())
}
