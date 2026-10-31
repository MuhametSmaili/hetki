package tui

import (
	"testing"

	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestSwitchingToTreeExpandsAllBranches(t *testing.T) {
	for _, test := range []struct {
		name           string
		query          string
		returnFromTree bool
	}{
		{name: "first entry unfiltered"},
		{name: "first entry filtered", query: "editor"},
		{name: "return after collapse unfiltered", returnFromTree: true},
		{name: "return after collapse filtered", query: "editor", returnFromTree: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tree := interactionSnapshot()
			flat := flatSnapshot(2)
			preferred := list.ItemID{ID: "window-1"}
			m := newModel(flat, nil)
			if test.returnFromTree {
				m = newModel(tree, nil)
				m.view = ViewTree
				require.True(t, m.items.CollapseAll())
				m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &flat, View: ViewFlat}})
				require.NoError(t, m.err)
			}
			m.setQuery(test.query)

			m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{
				Snapshot: &tree, View: ViewTree, SelectItemID: preferred,
			}})

			require.NoError(t, m.err)
			require.Equal(t, test.query, m.items.Query())
			require.Equal(t, preferred, selectedNodeID(m))
			require.True(t, m.items.Expansion()[list.ItemID{ID: "session-1"}])
			require.True(t, m.items.Expansion()[list.ItemID{ID: "session-2"}])

			m.setQuery("")
			require.Len(t, m.items.Rows(), 5)
		})
	}
}

func TestTreeRefreshPreservesCollapsedBranches(t *testing.T) {
	for _, test := range []struct {
		name string
		view ViewMode
	}{
		{name: "implicit view"},
		{name: "explicit tree view", view: ViewTree},
	} {
		t.Run(test.name, func(t *testing.T) {
			tree := interactionSnapshot()
			m := newModel(tree, nil)
			m.view = ViewTree
			require.True(t, m.items.CollapseAll())

			m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &tree, View: test.view}})

			require.NoError(t, m.err)
			require.False(t, m.items.Expansion()[list.ItemID{ID: "session-2"}])
			require.Len(t, m.items.Rows(), 4)
		})
	}
}
