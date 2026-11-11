package search_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/search"
	"github.com/sahilm/fuzzy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cancel at a deterministic cooperative checkpoint rather than racing a timer
// against a machine-dependent amount of matching work.
type checkpointContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *checkpointContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCancellationDiscardsPreparedAndScoredPartialData(t *testing.T) {
	entries := benchmarkEntries(10_000, 5)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prepareCtx := &checkpointContext{Context: ctx, cancel: cancel, remaining: 50}

	partialIndex, prepareErr := search.PrepareContext(prepareCtx, entries)
	index := search.Prepare(entries)
	scoreCtx, cancelScore := context.WithCancel(t.Context())
	defer cancelScore()
	scores, scoreErr := index.ScoresContext(&checkpointContext{Context: scoreCtx, cancel: cancelScore, remaining: 1000}, "project")

	require.ErrorIs(t, prepareErr, context.Canceled)
	require.Nil(t, partialIndex)
	require.ErrorIs(t, scoreErr, context.Canceled)
	require.Nil(t, scores)
	require.Equal(t, referenceScores(entries[:100], "project"), index.Scores("project")[:100])
}

func TestTextBudgetBatchesPreserveLongUnicodeFields(t *testing.T) {
	entries := [][]search.Field{
		{{Tier: search.Primary, Text: strings.Repeat("é東京-", 10_000) + "api"}},
		{{Tier: search.Secondary, Text: strings.Repeat("x", 15_000) + "api"}},
		{{Tier: search.Primary, Text: "api"}},
	}
	index := search.Prepare(entries)

	scores, err := index.ScoresContext(t.Context(), "api")

	require.NoError(t, err)
	require.Equal(t, referenceScores(entries, "api"), scores)
}

func TestScoresPreserveIndependentFieldMatching(t *testing.T) {
	for _, count := range []int{0, 1, 63, 64, 65, 129} {
		t.Run(fmt.Sprintf("%d fields", count), func(t *testing.T) {
			entries := scoringEntries(count)
			index := search.Prepare(entries)
			for _, query := range []string{"api", " \tAPI\n", "aaa", "é", "σ", " Σ ", "東京", "Straße", "missing", strings.Repeat("ab", 64), "", " \n"} {
				t.Run(query, func(t *testing.T) {
					t.Parallel()
					want := referenceScores(entries, query)

					scores := index.Scores(query)

					require.Equal(t, want, scores)
				})
			}
		})
	}
}

func scoringEntries(count int) [][]search.Field {
	texts := []string{"api", "ApiServer", "api/api", "aaaa", "école Straße Σσς", "東京", "hidden\x00api", "\xffapi", strings.Repeat("ab", 128)}
	var entry []search.Field
	for i := range count {
		tier := search.Primary
		if count > 1 && i >= count/2 {
			tier = search.Secondary
		}
		entry = append(entry, search.Field{Tier: tier, Text: texts[i%len(texts)]})
	}
	return [][]search.Field{nil, entry, {{Tier: search.Secondary, Text: "api"}}, entry, nil}
}

// This deliberately scores one field at a time, independently of the prepared
// batching implementation, to pin the existing matcher and tier semantics.
func referenceScores(entries [][]search.Field, query string) []int {
	scores := make([]int, len(entries))
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return scores
	}
	for i, fields := range entries {
		for _, field := range fields {
			matches := fuzzy.FindNoSort(query, []string{field.Text})
			if len(matches) == 0 {
				continue
			}
			priority := 1000
			if field.Tier == search.Primary {
				priority = 2000
			}
			quality := min(max(matches[0].Score, 0), 999)
			scores[i] = max(scores[i], priority+quality)
		}
	}
	return scores
}

func TestPreparedIndexOwnsEntryOrderAndResults(t *testing.T) {
	entries := [][]search.Field{
		{{Tier: search.Primary, Text: "api"}},
		{{Tier: search.Secondary, Text: "api"}},
	}
	want := referenceScores(entries, "api")
	index := search.Prepare(entries)

	entries[0] = nil
	entries[1] = nil
	first := index.Scores("api")
	second := index.Scores("other")
	firstBeforeMutation := slices.Clone(first)
	first[0] = 0
	third := index.Scores("api")

	assert.Equal(t, want, firstBeforeMutation, "later queries must not overwrite earlier results")
	assert.Equal(t, []int{0, 0}, second)
	assert.Equal(t, want, third, "neither outer entry changes nor result changes may modify the index")
}
