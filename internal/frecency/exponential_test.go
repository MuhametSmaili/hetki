package frecency

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreRevisitAddsOneToDecayedHistory(t *testing.T) {
	for _, tc := range []struct {
		name string
		days int
		want float64
	}{
		{name: "one week", days: 7, want: 5},
		{name: "two weeks", days: 14, want: 3},
		{name: "four weeks", days: 28, want: 1.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.json")
			used := time.Unix(20_000_000, 0)
			writeStateFixture(t, path, []Record{{Path: "/repo", Session: "work", Rank: 8, LastUsed: used.Unix()}})
			store := NewStore(path)
			now := used.Add(time.Duration(tc.days) * 24 * time.Hour)
			store.now = func() time.Time { return now }

			err := store.Record(t.Context(), "/repo", "work")

			require.NoError(t, err)
			records, err := store.Load()
			require.NoError(t, err)
			require.Equal(t, []Record{{Path: "/repo", Session: "work", Rank: tc.want, LastUsed: now.Unix()}}, records)
		})
	}
}

func TestStoreClockRollbackRebasesVisitsSoTheyResumeDecaying(t *testing.T) {
	now := time.Unix(20_000_000, 0)
	for _, tc := range []struct {
		name     string
		lastUsed int64
	}{
		{name: "one week ahead", lastUsed: now.Add(7 * 24 * time.Hour).Unix()},
		{name: "four weeks ahead", lastUsed: now.Add(28 * 24 * time.Hour).Unix()},
		{name: "extreme future timestamp", lastUsed: math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.json")
			writeStateFixture(t, path, []Record{{Path: "/repo", Session: "work", Rank: 4, LastUsed: tc.lastUsed}})
			store := NewStore(path)
			store.now = func() time.Time { return now }

			err := store.Record(t.Context(), "/repo", "work")

			require.NoError(t, err)
			records, err := store.Load()
			require.NoError(t, err)
			require.Equal(t, []Record{{Path: "/repo", Session: "work", Rank: 5, LastUsed: now.Unix()}}, records)
			twoWeeksLater := now.Add(14 * 24 * time.Hour)

			store.now = func() time.Time { return twoWeeksLater }

			err = store.Record(t.Context(), "/repo", "work")

			require.NoError(t, err)
			records, err = store.Load()
			require.NoError(t, err)
			require.Equal(t, []Record{{Path: "/repo", Session: "work", Rank: 2.25, LastUsed: twoWeeksLater.Unix()}}, records)
		})
	}
}

func TestScoreIsContinuousAcrossFormerBucketBoundaries(t *testing.T) {
	used := time.Unix(20_000_000, 0)
	record := Record{Rank: 8, LastUsed: used.Unix()}

	for _, boundary := range []time.Duration{time.Hour, 24 * time.Hour, 7 * 24 * time.Hour} {
		before := Score(record, used.Add(boundary-time.Second))
		after := Score(record, used.Add(boundary))

		require.Greater(t, before, after)
		require.Less(t, before-after, 0.00001)
	}
}

func TestScoreExtremeElapsedTimeUnderflowsToZero(t *testing.T) {
	record := Record{Rank: 8, LastUsed: math.MinInt64}

	got := Score(record, time.Unix(math.MaxInt64, 0))

	require.Zero(t, got)
}

func TestStoreRankSaturatesAtLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	writeStateFixture(t, path, []Record{{Path: "/repo", Session: "work", Rank: 10000, LastUsed: 100}})
	store := NewStore(path)
	store.now = func() time.Time { return time.Unix(100, 0) }

	err := store.Record(t.Context(), "/repo", "work")

	require.NoError(t, err)
	records, err := store.Load()
	require.NoError(t, err)
	require.InDelta(t, 10000.0, records[0].Rank, 1e-12)
}
