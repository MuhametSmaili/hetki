package frecency

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreCapacityEvictsTheWeakestOtherRecord(t *testing.T) {
	for _, tc := range []struct {
		name       string
		staleIndex int
		wantGone   string
	}{
		{name: "decayed contribution beats raw rank", staleIndex: 5000, wantGone: "/repo/05000"},
		{name: "equal scores use deterministic keys", staleIndex: -1, wantGone: "/repo/09999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.json")
			now := time.Unix(20_000_000, 0)
			records := benchmarkRecords(2)
			for i := range records {
				records[i].LastUsed = now.Unix()
			}
			if tc.staleIndex >= 0 {
				records[tc.staleIndex].Rank = 8
				records[tc.staleIndex].LastUsed = now.Add(-56 * 24 * time.Hour).Unix()
			}
			writeStateFixture(t, path, records)
			store := NewStore(path)
			store.now = func() time.Time { return now }

			err := store.Record(t.Context(), "/new-project", "new-session")

			require.NoError(t, err)
			records, err = store.Load()
			require.NoError(t, err)
			require.Len(t, records, 10000)
			scores := NewScores(records, now)
			require.InDelta(t, 1.0, scores.Record("/new-project", "new-session"), 1e-12)
			require.Zero(t, scores.Path(tc.wantGone))
		})
	}
}

func TestStoreLoadRejectsExcessiveRecordCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	records := append(benchmarkRecords(1), Record{Path: "/extra", Session: "work", Rank: 1})
	writeRawStateFixture(t, path, records)

	_, err := NewStore(path).Load()

	require.Error(t, err)
}

func TestStoreAcceptsPositiveFractionalContribution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	writeStateFixture(t, path, []Record{{Path: "/repo", Session: "work", Rank: 0.25, LastUsed: 100}})
	store := NewStore(path)
	store.now = func() time.Time { return time.Unix(100, 0) }

	err := store.Record(t.Context(), "/repo", "work")

	require.NoError(t, err)
	records, err := store.Load()
	require.NoError(t, err)
	require.InDelta(t, 1.25, records[0].Rank, 1e-12)
}
