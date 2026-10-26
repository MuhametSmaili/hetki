package buildinfo

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	recorded := &debug.BuildInfo{
		Main:     debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}},
	}
	for _, tc := range []struct {
		name                 string
		version, commit      string
		info                 *debug.BuildInfo
		wantVersion, wantSHA string
	}{
		{"fills empty values from the toolchain", "", "", recorded, "v1.2.3", "abc123"},
		{"keeps linker values", "v9.9.9", "fedcba", recorded, "v9.9.9", "fedcba"},
		{"keeps explicit development values", "dev", "unknown", recorded, "dev", "unknown"},
		{"ignores devel module version", "", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotVersion, gotCommit := resolve(tc.version, tc.commit, tc.info)

			require.Equal(t, tc.wantVersion, gotVersion)
			require.Equal(t, tc.wantSHA, gotCommit)
		})
	}
}
