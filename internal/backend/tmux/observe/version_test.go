package observe

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeaderLayoutForAuditedVersions(t *testing.T) {
	cases := []struct {
		version string
		want    headerLayout
	}{
		{"3.2", headerLegacy},
		{"3.2a", headerLegacy},
		{"3.3", headerLegacy},
		{"3.3a", headerLegacy},
		{"3.4", headerLegacy},
		{"3.5", headerLegacy},
		{"3.5a", headerLegacy},
		{"3.6", headerCurrent},
		{"3.6a", headerCurrent},
		{"3.6b", headerCurrent},
		{"3.7", headerCurrent},
		{"3.7a", headerCurrent},
		{"3.7b", headerCurrent},
		{"3.7c", headerCurrent},
		{"3.8", headerCurrent},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			layout, err := headerLayoutForVersion(tc.version)

			require.NoError(t, err)
			require.Equal(t, tc.want, layout)
		})
	}
}

func TestHeaderLayoutRejectsUnauditedVersions(t *testing.T) {
	cases := []struct {
		name    string
		version string
	}{
		{"missing", ""},
		{"below minimum", "3.1c"},
		{"unknown patch", "3.2b"},
		{"future patch", "3.8a"},
		{"future minor", "3.9"},
		{"double digit minor", "3.10"},
		{"future major", "4.0"},
		{"development", "next-3.8"},
		{"prerelease", "3.8-rc"},
		{"vendor suffix", "3.5a-vendor"},
		{"CLI prefix", "tmux 3.8"},
		{"whitespace", "3.8\n"},
		{"malformed", "3..8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout, err := headerLayoutForVersion(tc.version)

			require.Error(t, err)
			require.Zero(t, layout)
		})
	}
}
