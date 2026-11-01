//go:build integration

package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeSwitchMovesInvokingClientNotStatusObserver(t *testing.T) {
	for _, level := range []string{"session", "window", "pane", "linked pane", "attach", "popup"} {
		t.Run(level, func(t *testing.T) {
			fixture := newNavigationFixture(t)
			owner := fixture.attach(t, fixture.source.SessionID)
			_, observer := fixture.observe(t)
			target := fixture.destinationAt(t, level)
			if level == "popup" {
				t.Setenv("TMUX_PANE", "")
			}

			var err error
			if level == "attach" {
				err = fixture.backend.Attach(fixture.ctx, target.SessionID)
			} else {
				err = fixture.backend.Switch(fixture.ctx, target)
			}

			require.NoError(t, err)
			actual := fixture.clientTarget(t, owner)
			assert.Equal(t, target.SessionID, actual.SessionID)
			if target.WindowID != "" {
				assert.Equal(t, target.WindowID, actual.WindowID)
			}
			if target.PaneID != "" {
				assert.Equal(t, target.PaneID, actual.PaneID)
			}
			assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
		})
	}
}

func TestNativeSwitchRetainsInvokingClientAfterObserverReconnect(t *testing.T) {
	fixture := newNavigationFixture(t)
	owner := fixture.attach(t, fixture.source.SessionID)
	connection, _ := fixture.observe(t)
	require.NoError(t, fixture.backend.Switch(fixture.ctx, fixture.destination))
	require.Equal(t, fixture.destination, fixture.clientTarget(t, owner))
	require.NoError(t, connection.Close())
	_, observer := fixture.observe(t)

	err := fixture.backend.Switch(fixture.ctx, fixture.source)

	require.NoError(t, err)
	assert.Equal(t, fixture.source, fixture.clientTarget(t, owner))
	assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
}

func TestNativeSwitchRejectsUnidentifiedInvokingClient(t *testing.T) {
	for _, scenario := range []string{"ambiguous", "ambiguous popup", "missing owner", "invalid session", "invalid pane", "stale pane"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newNavigationFixture(t)
			var owners []string
			if scenario != "missing owner" {
				owners = append(owners, fixture.attach(t, fixture.source.SessionID))
			}
			switch scenario {
			case "ambiguous", "ambiguous popup":
				owners = append(owners, fixture.attach(t, fixture.source.SessionID))
				if scenario == "ambiguous popup" {
					t.Setenv("TMUX_PANE", "")
				}
			case "invalid session":
				t.Setenv("TMUX", "invalid")
			case "invalid pane":
				t.Setenv("TMUX_PANE", "invalid")
			case "stale pane":
				t.Setenv("TMUX_PANE", "%99999")
			}
			_, observer := fixture.observe(t)

			err := fixture.backend.Switch(fixture.ctx, fixture.destination)

			require.ErrorContains(t, err, "unique invoking tmux client")
			for _, owner := range owners {
				assert.Equal(t, fixture.source, fixture.clientTarget(t, owner))
			}
			assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
		})
	}
}

func TestNativeSwitchDoesNotRebindAfterOwnerDetaches(t *testing.T) {
	fixture := newNavigationFixture(t)
	owner := fixture.attach(t, fixture.source.SessionID)
	_, observer := fixture.observe(t)
	require.NoError(t, fixture.backend.Switch(fixture.ctx, fixture.destination))
	fixture.run(t, "detach-client", "-t", owner)
	replacement := fixture.attach(t, fixture.source.SessionID)

	err := fixture.backend.Switch(fixture.ctx, fixture.destination)

	require.ErrorContains(t, err, "no longer attached")
	assert.Equal(t, fixture.source, fixture.clientTarget(t, replacement))
	assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
}

func TestNativeSwitchFailsIfOwnerDetachesAtDispatch(t *testing.T) {
	fixture := newNavigationFixture(t)
	owner := fixture.attach(t, fixture.source.SessionID)
	_, observer := fixture.observe(t)
	fixture.backend.client = navigationDispatchClient{Client: fixture.backend.client, before: func() {
		fixture.run(t, "detach-client", "-t", owner)
	}}

	err := fixture.backend.Switch(fixture.ctx, fixture.destination)

	require.ErrorContains(t, err, "can't find client: "+owner)
	assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
}
