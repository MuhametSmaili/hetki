package frecency

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultStoreStartsFreshRegardlessOfLegacyHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		contents  string
		directory bool
	}{
		{name: "valid old history", contents: `{"version":1,"records":[{"path":"/repo","session":"work","rank":400,"last_used":20000000}]}`},
		{name: "corrupt old history", contents: `{"version":`},
		{name: "unsupported old version", contents: `{"version":99,"records":[]}`},
		{name: "old path is a directory", directory: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)
			legacyPath := filepath.Join(stateHome, "hetki", "frecency.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0700))
			if tc.directory {
				require.NoError(t, os.Mkdir(legacyPath, 0700))
			} else {
				require.NoError(t, os.WriteFile(legacyPath, []byte(tc.contents), 0600))
			}
			store, err := DefaultStore()
			require.NoError(t, err)
			store.now = func() time.Time { return time.Unix(20_000_000, 0) }

			records, err := store.Load()

			require.NoError(t, err)
			require.Empty(t, records)
			_, err = os.Stat(filepath.Join(stateHome, "hetki", "frecency-v2.json"))
			require.ErrorIs(t, err, os.ErrNotExist, "opening an empty picker must not create history")

			err = store.Record(t.Context(), "/repo", "work")

			require.NoError(t, err)
			fresh, err := DefaultStore()
			require.NoError(t, err)
			records, err = fresh.Load()
			require.NoError(t, err)
			require.Equal(t, []Record{{Path: "/repo", Session: "work", Rank: 1, LastUsed: 20_000_000}}, records)
		})
	}
}
