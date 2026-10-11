package profile_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/profile"
	"github.com/stretchr/testify/require"
)

const screenYAML = `version: 2
id: fixture
match: {command: fixture}
source:
  type: screen
  screen:
    poll_interval: 2s
    region: {last_nonblank_rows: 12}
    rules:
      - state: working
        all: [{contains: WORKING}]
      - state: needs-input
        all: [{contains: APPROVAL}]
      - state: idle
        all: [{contains: READY}]
`

const protocolYAML = `version: 2
id: explicit
match: {command: explicit}
source: {type: protocol}
`

func TestLoadSelectsTypedSources(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		source profile.Source
	}{
		{"screen", screenYAML, profile.Screen},
		{"protocol", protocolYAML, profile.Protocol},
		{"legacy protocol", strings.Replace(protocolYAML, "type: protocol", "type: osc, protocol: osc7501, scope: root", 1), profile.Protocol},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := profile.Load(strings.NewReader(tc.text))

			require.NoError(t, err)
			require.Equal(t, tc.source, p.Source)
			require.Len(t, p.Generation, 64)
			if tc.source == profile.Screen {
				require.Equal(t, 2*time.Second, p.PollInterval)
				state, err := p.Screen.Evaluate("APPROVAL")
				require.NoError(t, err)
				require.Equal(t, status.Blocked, state)
			} else {
				require.Nil(t, p.Screen)
				require.Zero(t, p.PollInterval)
			}
		})
	}
}

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"old version":       strings.Replace(screenYAML, "version: 2", "version: 1", 1),
		"unknown field":     screenYAML + "unknown: true\n",
		"nested field":      strings.Replace(screenYAML, "contains: READY", "contain: READY", 1),
		"duplicate field":   screenYAML + "version: 2\n",
		"duplicate matcher": strings.Replace(screenYAML, "contains: READY", "contains: READY, contains: OTHER", 1),
		"extra document":    screenYAML + "---\n" + protocolYAML,
		"empty document":    screenYAML + "---\n",
		"glob command":      strings.Replace(screenYAML, "command: fixture", "command: '*'", 1),
		"path command":      strings.Replace(screenYAML, "command: fixture", "command: /bin/fixture", 1),
		"long id":           strings.Replace(screenYAML, "id: fixture", "id: "+strings.Repeat("a", 65), 1),
		"missing rules":     strings.Split(screenYAML, "    rules:")[0],
		"unsupported state": strings.Replace(screenYAML, "state: idle", "state: done", 1),
		"bad regex":         strings.Replace(screenYAML, "contains: READY", "regex: '['", 1),
		"no interval":       strings.Replace(screenYAML, "    poll_interval: 2s\n", "", 1),
		"short interval":    strings.Replace(screenYAML, "2s", "1ms", 1),
		"long interval":     strings.Replace(screenYAML, "2s", "61s", 1),
		"oversized":         strings.Repeat(" ", profile.MaxProfileBytes+1),
		"alias":             strings.Replace(strings.Replace(screenYAML, "id: fixture", "id: &name fixture", 1), "command: fixture", "command: *name", 1),
		"merge":             strings.Replace(screenYAML, "match: {command: fixture}", "match: {<<: {command: fixture}}", 1),
		"null":              strings.Replace(protocolYAML, "type: protocol", "type: protocol, screen: null", 1),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := profile.Load(strings.NewReader(text))

			require.Error(t, err)
			require.Empty(t, p)
		})
	}
}

func TestLoadRejectsMixedSourceSettings(t *testing.T) {
	for name, source := range map[string]string{
		"unsupported":     "type: hook",
		"protocol scope":  "type: protocol, scope: root",
		"protocol screen": "type: protocol, screen: {}",
		"screen absent":   "type: screen",
		"screen protocol": "type: screen, screen: {}, protocol: ''",
		"legacy number":   "type: osc, protocol: osc133, scope: root",
		"legacy scope":    "type: osc, protocol: osc7501, scope: children",
		"legacy missing":  "type: osc, protocol: osc7501",
		"legacy mixed":    "type: osc, protocol: osc7501, scope: root, screen: {}",
	} {
		t.Run(name, func(t *testing.T) {
			text := strings.Replace(protocolYAML, "type: protocol", source, 1)

			p, err := profile.Load(strings.NewReader(text))

			require.Error(t, err)
			require.Empty(t, p)
		})
	}
}

func TestGenerationChangesWithConfiguration(t *testing.T) {
	first, err := profile.Load(strings.NewReader(screenYAML))
	require.NoError(t, err)
	same, err := profile.Load(strings.NewReader(screenYAML))
	require.NoError(t, err)

	changed, err := profile.Load(strings.NewReader(strings.Replace(screenYAML, "contains: READY", "contains: NEW", 1)))

	require.NoError(t, err)
	require.Equal(t, first.Generation, same.Generation)
	require.NotEqual(t, first.Generation, changed.Generation)
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestLoadPreservesReadFailure(t *testing.T) {
	failure := errors.New("read failed")
	reader := io.MultiReader(strings.NewReader(screenYAML), failingReader{failure})

	_, err := profile.Load(reader)

	require.ErrorIs(t, err, failure)
}
