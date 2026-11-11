package tui

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func receiveSearch[T any](t testing.TB, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for search worker")
		var zero T
		return zero
	}
}

func BenchmarkSearchSubmission(b *testing.B) {
	for _, count := range []int{100, 10_000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := newModel(searchBenchmarkSnapshot(count), nil)
			m.items.DeferSearch()
			started, release := make(chan struct{}, 1), make(chan struct{})
			m.search.worker = newSearchWorker(b.Context(), func(ctx context.Context, _ list.SearchRequest) (list.SearchResult, error) {
				started <- struct{}{}
				<-ctx.Done()
				<-release
				return list.SearchResult{}, ctx.Err()
			})
			b.Cleanup(func() {
				close(release)
				m.search.worker.close()
			})
			m.setQuery("initial")
			m.syncSearch()
			receiveSearch(b, started)
			b.ReportAllocs()

			for b.Loop() {
				m.setQuery("project")
				m.syncSearch()
				m.setQuery("project-00042")
				m.syncSearch()
			}
		})
	}
}

func BenchmarkSearchWorkerRoundtrip(b *testing.B) {
	for _, count := range []int{100, 10_000} {
		for _, mode := range []string{"synchronous", "worker"} {
			b.Run(fmt.Sprintf("%d/%s", count, mode), func(b *testing.B) {
				m := newModel(searchBenchmarkSnapshot(count), nil)
				if mode == "worker" {
					m.items.DeferSearch()
					m.search.worker = newSearchWorker(b.Context(), nil)
					b.Cleanup(m.search.worker.close)
				}
				benchmarkWorkerQuery(b, &m)
				b.ReportAllocs()

				for b.Loop() {
					benchmarkWorkerQuery(b, &m)
				}
			})
		}
	}
}

func benchmarkWorkerQuery(b *testing.B, m *model) {
	b.Helper()
	m.setQuery("project")
	m.syncSearch()
	if m.search.worker != nil {
		result := <-m.search.worker.results
		if result.err != nil || !m.items.AcceptSearch(result.result) {
			b.Fatalf("search failed: %v", result.err)
		}
	}
	m.setQuery("")
	m.syncSearch()
}

func searchBenchmarkSnapshot(count int) list.Snapshot {
	snapshot := list.Snapshot{Items: make([]list.Item, count)}
	for i := range snapshot.Items {
		text := fmt.Sprintf("project-%05d", i)
		snapshot.Items[i] = list.Item{ID: list.ItemID{ID: text}, Primary: text,
			SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: text}}}
	}
	return snapshot
}

func TestSearchWorkerCancelsAndCoalescesTyping(t *testing.T) {
	items, err := list.New(interactionSnapshot())
	require.NoError(t, err)
	items.DeferSearch()
	started := make(chan list.SearchKey, 2)
	cancelled := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	calls := 0
	w := newSearchWorker(t.Context(), func(ctx context.Context, request list.SearchRequest) (list.SearchResult, error) {
		calls++
		started <- request.Key()
		if calls == 1 {
			<-ctx.Done()
			close(cancelled)
			<-release
			// Simulate completion racing cancellation: the worker must discard it.
			return request.Evaluate(context.WithoutCancel(ctx))
		}
		return request.Evaluate(ctx)
	})
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		w.close()
	})
	items.SetQuery("editor")
	first, _ := items.PendingSearch()
	w.submit(&first)
	require.Equal(t, first.Key(), receiveSearch(t, started))

	for i := range 100 {
		items.SetQuery(fmt.Sprintf("obsolete-%d", i))
		request, _ := items.PendingSearch()
		w.submit(&request)
	}
	items.SetQuery("server")
	latest, _ := items.PendingSearch()
	w.submit(&latest)
	receiveSearch(t, cancelled)
	releaseOnce.Do(func() { close(release) })
	key := receiveSearch(t, started)
	result := receiveSearch(t, w.results)
	w.close()

	require.Equal(t, latest.Key(), key)
	require.Equal(t, latest.Key(), result.key)
	require.NoError(t, result.err)
	require.True(t, items.AcceptSearch(result.result))
	row, ok := items.Selected()
	require.True(t, ok)
	require.Equal(t, "window-2", row.Item.ID.ID)
	require.Equal(t, 2, calls, "intermediate requests must never be evaluated")
}

func TestSearchWorkerShutdownJoinsRunningEvaluation(t *testing.T) {
	items, err := list.New(interactionSnapshot())
	require.NoError(t, err)
	items.DeferSearch()
	items.SetQuery("server")
	request, _ := items.PendingSearch()
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	w := newSearchWorker(t.Context(), func(ctx context.Context, _ list.SearchRequest) (list.SearchResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return list.SearchResult{}, ctx.Err()
	})
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		w.close()
	})
	w.submit(&request)
	receiveSearch(t, started)
	closed := make(chan struct{})

	go func() {
		w.close()
		close(closed)
	}()
	receiveSearch(t, cancelled)
	select {
	case <-closed:
		t.Fatal("shutdown returned before the evaluation stopped")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	receiveSearch(t, closed)

	_, ok := <-w.results
	require.False(t, ok, "shutdown closes the result waiter without publishing cancelled work")
}

func TestSearchWorkerClearsUndeliveredResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items, err := list.New(interactionSnapshot())
		require.NoError(t, err)
		items.DeferSearch()
		items.SetQuery("editor")
		request, _ := items.PendingSearch()
		w := newSearchWorker(t.Context(), nil)
		t.Cleanup(w.close)
		w.submit(&request)
		synctest.Wait()
		require.Len(t, w.results, 1)

		w.submit(nil)
		synctest.Wait()

		require.Empty(t, w.results)
		w.close()
		_, ok := <-w.results
		require.False(t, ok)
	})
}

func TestSearchWorkerExitsWhenParentIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := newSearchWorker(ctx, nil)
	t.Cleanup(w.close)

	cancel()
	receiveSearch(t, w.done)

	_, ok := <-w.results
	require.False(t, ok)
}
