// Package search scores immutable collections independently of presentation,
// item identity, hierarchy and data acquisition.
package search

import (
	"context"
	"slices"
	"strings"

	"github.com/sahilm/fuzzy"
)

// Tier determines field precedence. A primary match outranks any secondary match.
type Tier uint8

const (
	Primary Tier = iota + 1
	Secondary
)

// Field is searchable text. Tier must be Primary or Secondary.
type Field struct {
	Tier Tier
	Text string
}

const (
	maxQuality = 999
	batchSize  = 64
	batchBytes = 16 * 1024
)

// Index owns entry references and borrows immutable search fields. It retains
// no display data or action values. A zero Index is empty.
// Prepared indexes are immutable and may be shared by concurrent readers.
type Index struct {
	entries    [][]Field
	fieldCount int
}

// Prepare copies the outer entry slice, preserving entry positions including
// entries with no fields. Callers must not mutate the borrowed field slices for
// the index's lifetime. Additional retained storage is proportional to entry
// count, not field count or text length; field data is not copied or normalized.
func Prepare(entries [][]Field) *Index {
	index, _ := PrepareContext(context.Background(), entries)
	return index
}

// PrepareContext is Prepare with cooperative cancellation during metadata
// traversal. Cancellation never returns a partially prepared index.
func PrepareContext(ctx context.Context, entries [][]Field) (*Index, error) {
	fieldCount := 0
	for i, entry := range entries {
		if i%64 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		fieldCount += len(entry)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := &Index{entries: slices.Clone(entries), fieldCount: fieldCount}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return index, nil
}

// Scores returns one score per entry in input order, without sorting. Zero means
// no match; larger scores are stronger. Only the best field contributes. Queries
// are trimmed and lowercased; an empty query has no matches (no active filter).
// The caller owns the result; later or concurrent calls cannot change it.
func (index *Index) Scores(query string) []int {
	scores, _ := index.ScoresContext(context.Background(), query)
	return scores
}

// ScoresContext discards partial scores on cancellation. Checks occur between
// batches bounded by field count and text bytes; a single field is indivisible
// because the underlying matcher does not support interruption within a field.
func (index *Index) ScoresContext(ctx context.Context, query string) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scores := make([]int, len(index.entries))
	if query == "" {
		return scores, nil
	}
	scorer := queryScorer{query: query, scores: scores, batch: make(fields, 0, min(batchSize, index.fieldCount))}
	for owner, entry := range index.entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, field := range entry {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			scorer.add(ctx, field, owner)
		}
	}
	if len(scorer.batch) > 0 && ctx.Err() == nil {
		scorer.flush(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return scores, nil
}

type queryScorer struct {
	query  string
	scores []int
	batch  fields
	bytes  int
}

func (s *queryScorer) add(ctx context.Context, field Field, owner int) {
	if len(s.batch) > 0 && s.bytes+len(field.Text) > batchBytes {
		s.flush(ctx)
	}
	s.batch = append(s.batch, indexedField{Field: field, owner: owner})
	s.bytes += len(field.Text)
	if len(s.batch) == cap(s.batch) || s.bytes >= batchBytes {
		s.flush(ctx)
	}
}

func (s *queryScorer) flush(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	for _, match := range fuzzy.FindFromNoSort(s.query, s.batch) {
		field := s.batch[match.Index]
		quality := min(max(match.Score, 0), maxQuality)
		priority := (int(Secondary) - int(field.Tier) + 1) * 1000
		s.scores[field.owner] = max(s.scores[field.owner], priority+quality)
	}
	s.batch = s.batch[:0]
	s.bytes = 0
}

type indexedField struct {
	Field
	owner int
}

type fields []indexedField

func (f fields) Len() int            { return len(f) }
func (f fields) String(i int) string { return f[i].Text }
