package statusobserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

const fixtureProfile = `version: 2
id: fixture
match: {command: cat}
source:
  type: screen
  screen:
    poll_interval: 30s
    region: {last_nonblank_rows: 4}
    rules:
      - state: idle
        all: [{contains: READY}]
      - state: working
        all: [{contains: WORKING}]
      - state: needs-input
        all: [{contains: APPROVAL}]
`

func writeConfiguredProfile(t *testing.T, name, text string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "hetki", "status.d")
	require.NoError(t, os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(text), 0600))
	return path
}

func TestProfileSelectorKeepsSourcesSeparate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeConfiguredProfile(t, "screen.yaml", fixtureProfile)
	writeConfiguredProfile(t, "protocol.yaml", "version: 2\nid: explicit\nmatch: {command: explicit}\nsource: {type: protocol}\n")
	catalog, err := loadProfiles(t.Context())
	require.NoError(t, err)
	selectProfile := profileSelector(catalog)

	selected := selectProfile(observe.Pane{Command: "cat"})
	state, err := selected.Evaluate("READY\n\n\n\n\n\n")
	explicit := selectProfile(observe.Pane{Command: "explicit"})
	missing := selectProfile(observe.Pane{Command: "other"})

	require.NoError(t, err)
	require.Equal(t, status.Idle, state)
	require.Equal(t, service.Screen, selected.Profile.Source)
	require.NotEmpty(t, selected.Profile.Generation)
	require.Equal(t, observe.Region{}, selected.Region)
	require.Equal(t, 30*time.Second, selected.PollInterval)
	require.Equal(t, service.Protocol, explicit.Profile.Source)
	require.NotEmpty(t, explicit.Profile.Generation)
	require.Nil(t, explicit.Evaluate)
	require.Equal(t, Selection{}, missing)
}
