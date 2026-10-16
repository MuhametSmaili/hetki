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
