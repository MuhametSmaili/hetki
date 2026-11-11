package list

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func acceptPending(t *testing.T, m *Model) {
	t.Helper()
	request, pending := m.PendingSearch()
	if !pending {
		return
	}
	result, err := request.Evaluate(t.Context())
	require.NoError(t, err)
	require.True(t, m.AcceptSearch(result))
}

func TestDeferredSearchPreservesRankingAndAllMatches(t *testing.T) {
	snapshot := filterFieldBenchmarkSnapshot(10_000, 5)
	for i := range snapshot.Items {
		if i%3 == 0 {
			snapshot.Items[i].SearchFields = []SearchField{{Tier: SearchSecondary, Text: "project"}}
		}
	}
	m, err := New(snapshot)
	require.NoError(t, err)
	m.DeferSearch()
	m.Resize(10)
	want := slices.Clone(snapshot.Items)
	slices.SortStableFunc(want, func(a, b Item) int {
		return referenceItemScore(b, "project") - referenceItemScore(a, "project")
	})
	wantIDs := make([]ItemID, len(want))
	for i := range want {
		wantIDs[i] = want[i].ID
	}

	m.SetQuery(" PROJECT ")
	acceptPending(t, &m)
	m.Bottom()

	require.Equal(t, wantIDs, m.RowIDs())
	require.Equal(t, wantIDs[len(wantIDs)-1], selectedID(m))
	require.Equal(t, 10_000, m.Len())
	require.Contains(t, rowIDs(m.VisibleRows()), selectedID(m))
}

func TestDeferredSearchRejectsChangedInputs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *Model)
	}{
		{"query", func(_ *testing.T, m *Model) { m.SetQuery("editor") }},
		{"scope", func(_ *testing.T, m *Model) { m.SetFilter("api", map[ItemID]struct{}{{ID: "shell"}: {}}) }},
		{"searchable details", func(_ *testing.T, m *Model) {
			m.SetDetails(map[ItemID]Details{{ID: "editor"}: {SearchFields: []SearchField{{Tier: SearchPrimary, Text: "api"}}}})
		}},
		{"snapshot", func(t *testing.T, m *Model) {
			t.Helper()
			require.NoError(t, m.Replace(flatFixture(), ItemID{ID: "server"}))
		}},
		{"clear", func(_ *testing.T, m *Model) { m.SetQuery("") }},
		{"another list", func(t *testing.T, m *Model) {
			t.Helper()
			replacement, err := New(nestedFixture())
			require.NoError(t, err)
			replacement.DeferSearch()
			replacement.SetQuery("api")
			*m = replacement
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := New(nestedFixture())
			require.NoError(t, err)
			m.DeferSearch()
			m.SetQuery("api")
			request, _ := m.PendingSearch()
			old, err := request.Evaluate(t.Context())
			require.NoError(t, err)

			tc.change(t, &m)
			accepted := m.AcceptSearch(old)
			acceptPending(t, &m)

			require.False(t, accepted)
			require.False(t, m.SearchPending())
			reference := m
			reference.deferred = false
			reference.applyFilter()
			require.Equal(t, reference.Rows(), m.Rows())
		})
	}
}

func TestDeferredDetailsPreserveSelectionAndRejectOldPresentation(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	m.DeferSearch()
	m.SetQuery("e")
	acceptPending(t, &m)
	require.True(t, m.Select(ItemID{ID: "server"}))
	details := map[ItemID]Details{{ID: "server"}: {
		Secondary: "first", SearchFields: []SearchField{{Tier: SearchPrimary, Text: "server"}},
	}}
	m.SetDetails(details)
	request, _ := m.PendingSearch()
	old, err := request.Evaluate(t.Context())
	require.NoError(t, err)

	m.SetDetails(map[ItemID]Details{{ID: "server"}: {Secondary: "second", SearchFields: details[ItemID{ID: "server"}].SearchFields}})
	accepted := m.AcceptSearch(old)
	acceptPending(t, &m)

	require.False(t, accepted)
	row, ok := m.Selected()
	require.True(t, ok)
	require.Equal(t, "server", row.Item.ID.ID)
	require.Equal(t, "second", row.Item.Secondary)
	requestAfter, _ := m.PendingSearch()
	require.Equal(t, request.key.dataset, requestAfter.key.dataset, "presentation-only changes reuse search preparation")
}

func TestDeferredSearchCancellationDoesNotPublishPartialRows(t *testing.T) {
	m, err := New(filterFieldBenchmarkSnapshot(10_000, 5))
	require.NoError(t, err)
	m.DeferSearch()
	m.SetQuery("project")
	request, _ := m.PendingSearch()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := request.Evaluate(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.False(t, m.AcceptSearch(result))
	require.True(t, m.SearchPending())
}

func TestWorkerPreparationSurvivesUnpublishedQueries(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	m.DeferSearch()
	var searcher Searcher
	m.SetQuery("editor")
	firstRequest, _ := m.PendingSearch()
	first, err := searcher.Evaluate(t.Context(), firstRequest)
	require.NoError(t, err)

	m.SetQuery("server")
	nextRequest, _ := m.PendingSearch()
	next, err := searcher.Evaluate(t.Context(), nextRequest)
	require.NoError(t, err)
	require.Same(t, first.index, next.index)
	require.True(t, m.AcceptSearch(next))
	require.Equal(t, ItemID{ID: "server"}, selectedID(m))

	require.NoError(t, m.Replace(flatFixture(), ItemID{}))
	replacementRequest, _ := m.PendingSearch()
	replacement, err := searcher.Evaluate(t.Context(), replacementRequest)
	require.NoError(t, err)
	require.NotSame(t, next.index, replacement.index)
	searcher.Reset()
	require.Nil(t, searcher.index)
}

func TestDeferredSelectionUsesNewResultsAndFallbackOrder(t *testing.T) {
	m, err := New(nestedFixture())
	require.NoError(t, err)
	m.DeferSearch()
	m.SetQuery("e")
	m.PreferSelection(ItemID{ID: "gone"}, ItemID{ID: "server"})

	acceptPending(t, &m)

	require.Equal(t, ItemID{ID: "server"}, selectedID(m))
}

func BenchmarkDeferredSearch(b *testing.B) {
	for _, count := range []int{100, 10_000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m, err := New(filterFieldBenchmarkSnapshot(count, 5))
			if err != nil {
				b.Fatal(err)
			}
			m.DeferSearch()
			benchmarkSearch(b, &m, "project")
			b.ReportAllocs()

			for b.Loop() {
				benchmarkSearch(b, &m, "project-00042")
				benchmarkSearch(b, &m, "project")
			}
		})
	}
}

func benchmarkSearch(b *testing.B, m *Model, query string) {
	b.Helper()
	m.SetQuery(query)
	request, _ := m.PendingSearch()
	result, err := request.Evaluate(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	m.AcceptSearch(result)
}
