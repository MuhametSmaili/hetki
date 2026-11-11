package list

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRowsRetainPresentationAcrossModelChanges(t *testing.T) {
	model, err := New(nestedFixture())
	require.NoError(t, err)
	model.ResizeRows(4, treeRowHeight)
	model.SetDetails(map[ItemID]Details{{ID: "editor"}: {
		Secondary: "/updated/editor", Trailing: "running",
		SearchFields: []SearchField{{Tier: SearchPrimary, Text: "editor"}},
	}})
	rows := model.Rows()
	visible := model.VisibleRows()
	wantRows := slices.Clone(rows)
	wantVisible := slices.Clone(visible)
	require.Equal(t, "/updated/editor", rows[1].Item.Secondary)
	require.Equal(t, "├─ ", rows[1].TreePrefix)

	model.SetQuery("api")
	model.SetDetails(nil)
	require.NoError(t, model.Replace(flatFixture(), ItemID{}))
	model.SetQuery("editor")

	require.Equal(t, wantRows, rows)
	require.Equal(t, wantVisible, visible)
	selected, ok := model.Selected()
	require.True(t, ok)
	require.Empty(t, selected.TreePrefix)
	require.Empty(t, selected.Item.Trailing)
	require.Equal(t, "~/code/editor", selected.Item.Secondary)
}
