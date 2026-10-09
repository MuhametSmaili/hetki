package list

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestItemFilterRetainsAncestorsAndCollapsedState(t *testing.T) {
	snapshot := nestedFixture()
	snapshot.ActiveItemID = ""
	m, err := New(snapshot)
	require.NoError(t, err)
	m.CollapseAll()
	included := map[ItemID]struct{}{"server": {}}

	m.SetFilter("server", included)
	delete(included, "server")
	err = m.Replace(m.Snapshot(), "server")
	filtered := rowIDs(m.Rows())
	m.SetFilter("", nil)

	require.NoError(t, err)
	require.Equal(t, []ItemID{"dev", "server"}, filtered)
	require.Equal(t, []ItemID{"dev", "prod"}, rowIDs(m.Rows()))
	require.Len(t, m.Snapshot().Items[0].Children, 2)
}

func TestItemFilterDistinguishesEmptySetFromUnrestricted(t *testing.T) {
	m, err := New(flatFixture())
	require.NoError(t, err)

	m.SetFilter("", map[ItemID]struct{}{})
	empty := rowIDs(m.Rows())
	m.SetFilter("", nil)

	require.Empty(t, empty)
	require.Equal(t, []ItemID{"editor", "server", "shell"}, rowIDs(m.Rows()))
}
