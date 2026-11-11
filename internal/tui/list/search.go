package list

import (
	"context"
	"slices"

	"github.com/MuhametSmaili/hetki/internal/search"
)

// SearchKey identifies a list instance, dataset revision and query operation.
// It is comparable; callers must treat its contents as opaque.
type SearchKey struct {
	owner        *byte
	dataset      uint64
	query        uint64
	presentation uint64
}

// itemProjection is immutable and shared by the model, worker and result rows.
// Retaining one projection avoids copying item payloads on every query.
type itemProjection struct{ items []Item }

// SearchRequest borrows immutable snapshot, scope and detail data. It contains
// no mutable navigation state and can be evaluated independently of the model.
type SearchRequest struct {
	key        SearchKey
	items      []Item
	included   map[ItemID]struct{}
	details    map[ItemID]Details
	index      *search.Index
	query      string
	projection *itemProjection
}

// SearchResult owns complete ranked rows and shares immutable search metadata.
// Its borrowed item contents obey the snapshot/details immutability contract.
type SearchResult struct {
	key        SearchKey
	rows       []rowRef
	index      *search.Index
	roots      int
	best       int
	projection *itemProjection
}

func (r SearchRequest) Key() SearchKey { return r.key }

// Evaluate prepares, matches and ranks off the UI loop. Cancellation returns no
// publishable result; the caller must still check revisions when accepting it.
func (r SearchRequest) Evaluate(ctx context.Context) (SearchResult, error) {
	var searcher Searcher
	return searcher.Evaluate(ctx, r)
}

// Searcher retains preparation for one dataset, including when a query is
// cancelled after preparation. A worker owns it; calls must be serialized.
// It retains one immutable projection and index, but no previous result rows.
type Searcher struct {
	key        SearchKey
	index      *search.Index
	projection *itemProjection
}

// Reset releases retained dataset references when the worker becomes idle.
func (s *Searcher) Reset() { *s = Searcher{} }

func (s *Searcher) Evaluate(ctx context.Context, r SearchRequest) (SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return SearchResult{}, err
	}
	if s.key.owner != r.key.owner || s.key.dataset != r.key.dataset {
		s.index, s.projection = r.index, nil
	}
	if s.key.presentation != r.key.presentation {
		s.projection = nil
	}
	s.key = r.key
	if s.projection == nil {
		s.projection = r.projection
	}
	if s.projection == nil {
		items := projectItems(ctx, r.items, r.included, r.details)
		if err := ctx.Err(); err != nil {
			return SearchResult{}, err
		}
		s.projection = &itemProjection{items: items}
	}
	items := s.projection.items
	if s.index == nil && ctx.Err() == nil {
		s.index = prepareSearch(ctx, items)
	}
	if err := ctx.Err(); err != nil {
		return SearchResult{}, err
	}
	scores, err := s.index.ScoresContext(ctx, r.query)
	if err != nil {
		return SearchResult{}, err
	}
	rows := rankedMatches(ctx, items, scores)
	result := SearchResult{key: r.key, rows: rows, index: s.index, projection: s.projection}
	result.summarize(ctx)
	if err := ctx.Err(); err != nil {
		return SearchResult{}, err
	}
	return result, nil
}

func (result *SearchResult) summarize(ctx context.Context) {
	bestScore := 0
	for i, row := range result.rows {
		if i%64 == 0 && ctx.Err() != nil {
			return
		}
		if row.depth == 0 {
			result.roots++
		}
		if row.score > bestScore {
			result.best, bestScore = i, row.score
		}
	}
}

// DeferSearch makes nonempty filters pending instead of evaluating them inline.
// The caller must evaluate PendingSearch requests and publish with AcceptSearch.
// Empty filters remain synchronous so clearing never waits behind a search.
func (m *Model) DeferSearch() { m.deferred = true }

func (m Model) SearchPending() bool { return m.pendingSearch }

func (m Model) PendingSearch() (SearchRequest, bool) {
	return SearchRequest{
		key: m.searchKey, items: m.snapshot.Items, included: m.included,
		details: m.details, index: m.search, query: m.query, projection: m.projection,
	}, m.pendingSearch
}

// AcceptSearch rejects obsolete results, even if their evaluation was not
// cancelled in time. Navigation and viewport state stay owned by the UI model.
func (m *Model) AcceptSearch(result SearchResult) bool {
	if !m.pendingSearch || result.key != m.searchKey {
		return false
	}
	m.rows, m.search, m.shownRoot = result.rows, result.index, result.roots
	m.projection = result.projection
	m.cursor = result.best
	m.pendingSearch = false
	m.PreferSelection(m.preferred...)
	m.preferred = nil
	m.reflow()
	return true
}

func (m *Model) invalidateSearch() {
	m.search = nil
	m.projection = nil
	m.searchKey.dataset++
}

// PreferSelection selects the first visible ID, or records the preference order
// to try when pending results arrive.
func (m *Model) PreferSelection(ids ...ItemID) {
	if m.pendingSearch {
		m.preferred = slices.Clone(ids)
		return
	}
	//nolint:modernize // Keep selection changes explicit rather than hiding them in a predicate.
	for _, id := range ids {
		if m.Select(id) {
			return
		}
	}
}
