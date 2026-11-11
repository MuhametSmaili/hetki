package list

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScopedQueryEditsAndClearRetainScope(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	m.DeferSearch()
	m.SetFilter("", map[ItemID]struct{}{{ID: "editor"}: {}})
	want := m.Rows()

	m.SetFilterQuery("editor")
	acceptPending(t, &m)
	require.Equal(t, ItemID{ID: "editor"}, selectedID(m))
	m.SetFilterQuery("server")
	acceptPending(t, &m)
	require.Empty(t, m.Rows())
	m.SetFilterQuery("")

	require.False(t, m.SearchPending())
	require.Equal(t, want, m.Rows())
}

func TestWorkerProjectionRefreshesWithoutMutatingPublishedRows(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	m.DeferSearch()
	m.SetFilter("editor", map[ItemID]struct{}{{ID: "editor"}: {}})
	var worker Searcher
	request, _ := m.PendingSearch()
	first, err := worker.Evaluate(t.Context(), request)
	require.NoError(t, err)
	require.True(t, m.AcceptSearch(first))
	before := m.Rows()

	m.SetFilterQuery("edit")
	request, _ = m.PendingSearch()
	second, err := worker.Evaluate(t.Context(), request)
	require.NoError(t, err)
	require.Same(t, first.projection, second.projection, "query edits reuse preparation")
	require.True(t, m.AcceptSearch(second))

	fields := []SearchField{{Tier: SearchPrimary, Text: "editor"}}
	m.SetDetails(map[ItemID]Details{{ID: "editor"}: {Secondary: "first", SearchFields: fields}})
	request, _ = m.PendingSearch()
	oldDetails, err := worker.Evaluate(t.Context(), request)
	require.NoError(t, err)
	m.SetDetails(map[ItemID]Details{{ID: "editor"}: {Secondary: "second", SearchFields: fields}})
	request, _ = m.PendingSearch()
	current, err := worker.Evaluate(t.Context(), request)
	require.NoError(t, err)

	require.False(t, m.AcceptSearch(oldDetails))
	require.True(t, m.AcceptSearch(current))
	require.Same(t, oldDetails.index, current.index)
	selected, _ := m.Selected()
	require.Equal(t, "second", selected.Item.Secondary)
	require.Equal(t, before, materializeRows(first.rows))
	m.SetFilterQuery("")
	selected, _ = m.Selected()
	require.Equal(t, "second", selected.Item.Secondary)
}
