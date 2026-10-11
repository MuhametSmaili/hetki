//go:build integration

package statusobserver

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/daemon"
	"github.com/stretchr/testify/require"
)

func TestNativeExecutableProfiles(t *testing.T) {
	executable := buildDaemonExecutable(t)
	t.Run("screen selection and hinted updates", func(t *testing.T) {
		testNativeProfileUpdates(t, executable)
	})
	t.Run("invalid configuration before launch", func(t *testing.T) {
		_, server, _, _, _ := nativeStreamsFixture(t)
		client := daemonClientFixture(t, server.Identity())
		writeConfiguredProfile(t, "bad.yaml", "version: invalid\n")

		_, err := EnsureDaemon(t.Context(), server, executable)
		_, pingErr := client.Ping(t.Context())

		require.ErrorContains(t, err, "bad.yaml")
		require.Error(t, pingErr)
	})
	t.Run("child validates and logs configuration", func(t *testing.T) {
		_, server, _, _, _ := nativeStreamsFixture(t)
		client := daemonClientFixture(t, server.Identity())
		writeConfiguredProfile(t, "bad.yaml", "version: invalid\n")
		descriptor, err := json.Marshal(server)
		require.NoError(t, err)

		err = ServeDaemon(t.Context(), server.Identity(), string(descriptor))
		log, readErr := os.ReadFile(client.LogPath())

		require.ErrorContains(t, err, "bad.yaml")
		require.NoError(t, readErr)
		require.Contains(t, string(log), `event="serve_failed"`)
	})
}

func testNativeProfileUpdates(t *testing.T, executable string) {
	t.Helper()
	runner, server, _, id, _ := nativeStreamsFixture(t)
	client := daemonClientFixture(t, server.Identity())
	path := writeConfiguredProfile(t, "fixture.yaml", fixtureProfile)
	_, err := EnsureDaemon(t.Context(), server, executable)
	require.NoError(t, err)
	info, err := client.Ping(t.Context())
	require.NoError(t, err)
	awaitDaemonSubscription(t, t.Context(), runner, info.PID)
	for _, tc := range []struct {
		text string
		want status.State
	}{
		{"READY\x1b]7501;state=working\a", status.Idle},
		{"WORKING", status.Working},
		{"APPROVAL\a", status.Blocked},
		{"UNRECOGNIZED", status.Unknown},
	} {
		_, err := runner.RunLimited(t.Context(), 4096, "send-keys", "-t", id, "-l", "--", "\x1b[2J\x1b[H"+tc.text+"\n")
		require.NoError(t, err)
		awaitDaemonPaneState(t, client, id, tc.want, "")
	}

	require.NoError(t, os.WriteFile(path, []byte("invalid: changed after startup\n"), 0600))
	_, err = EnsureDaemon(t.Context(), server, executable)
	require.NoError(t, err)
	reused, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.Equal(t, info, reused, "existing daemon retains its startup catalog")
	_, err = runner.RunLimited(t.Context(), 4096, "send-keys", "-t", id, "-l", "--", "\x1b[2J\x1b[HREADY\n")
	require.NoError(t, err)
	before := awaitDaemonPaneState(t, client, id, status.Idle, "")

	_, err = runner.RunLimited(t.Context(), 4096, "respawn-pane", "-k", "-t", id, "stty -echo; exec cat")
	require.NoError(t, err)
	after := awaitDaemonPaneState(t, client, id, status.Unknown, before.Generation)

	require.NotEqual(t, before, after)
}

func awaitDaemonPaneState(t *testing.T, client *daemon.Client, id string, want status.State, previous string) status.PaneRef {
	t.Helper()
	var found status.PaneRef
	require.Eventually(t, func() bool {
		snapshot, err := client.ReadStatus(t.Context())
		if err != nil {
			return false
		}
		for ref, value := range snapshot.All() {
			if ref.ID == id && ref.Generation != previous && value.At(time.Now()) == want {
				found = ref
				return true
			}
		}
		return false
	}, 4*time.Second, 10*time.Millisecond, "pane %s did not become %s", id, want)
	return found
}
