//go:build integration

package tmux

import (
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
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

func TestNativeSwitchKeepsSelectedWindowAfterReindex(t *testing.T) {
	fixture := newNavigationFixture(t)
	owner := fixture.attach(t, fixture.source.SessionID)
	_, observer := fixture.observe(t)
	otherWindow, _ := fixture.newWindow(t, fixture.destination.SessionID)
	fixture.backend.client = navigationDispatchClient{Client: fixture.backend.client, before: func() {
		fixture.run(t, "swap-window", "-d", "-s", fixture.destination.WindowID, "-t", otherWindow)
	}}

	err := fixture.backend.Switch(fixture.ctx, backend.Target{
		SessionID: fixture.destination.SessionID, WindowID: fixture.destination.WindowID,
	})

	require.NoError(t, err)
	assert.Equal(t, fixture.destination, fixture.clientTarget(t, owner))
	assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
}

func TestNativeSwitchKeepsSelectedPaneAfterReindex(t *testing.T) {
	fixture := newNavigationFixture(t)
	owner := fixture.attach(t, fixture.source.SessionID)
	_, observer := fixture.observe(t)
	otherPane := fixture.splitPane(t, fixture.destination.WindowID)
	fixture.backend.client = navigationDispatchClient{Client: fixture.backend.client, before: func() {
		fixture.run(t, "swap-pane", "-d", "-s", fixture.destination.PaneID, "-t", otherPane)
	}}

	err := fixture.backend.Switch(fixture.ctx, fixture.destination)

	require.NoError(t, err)
	assert.Equal(t, fixture.destination, fixture.clientTarget(t, owner))
	assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
}

func TestNativeSwitchKeepsLinkedOccurrenceAfterReindex(t *testing.T) {
	fixture := newNavigationFixture(t)
	owner := fixture.attach(t, fixture.source.SessionID)
	_, observer := fixture.observe(t)
	target := fixture.destinationAt(t, "linked pane")
	otherWindow, _ := fixture.newWindow(t, target.SessionID)
	otherPane := fixture.splitPane(t, target.WindowID)
	fixture.backend.client = navigationDispatchClient{Client: fixture.backend.client, before: func() {
		fixture.run(t, "swap-window", "-d", "-s", target.SessionID+":"+target.WindowID, "-t", otherWindow)
		fixture.run(t, "swap-pane", "-d", "-s", target.PaneID, "-t", otherPane)
	}}

	err := fixture.backend.Switch(fixture.ctx, target)

	require.NoError(t, err)
	assert.Equal(t, target, fixture.clientTarget(t, owner))
	assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
}

func TestNativeSwitchRejectsRelocatedOrRemovedDestinationAtDispatch(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, navigationFixture) func()
		wantErr string
	}{
		{
			name: "window removed with replacement at its old index",
			prepare: func(t *testing.T, f navigationFixture) func() {
				t.Helper()
				other, _ := f.newWindow(t, f.destination.SessionID)
				return func() {
					index := f.run(t, "display-message", "-p", "-t", f.destination.WindowID, "#{window_index}")
					f.run(t, "kill-window", "-t", f.destination.WindowID)
					f.run(t, "move-window", "-s", other, "-t", f.destination.SessionID+":"+index)
				}
			},
			wantErr: "can't find window",
		},
		{
			name: "pane removed with replacement at its old index",
			prepare: func(t *testing.T, f navigationFixture) func() {
				t.Helper()
				other := f.splitPane(t, f.destination.WindowID)
				index := f.run(t, "display-message", "-p", "-t", f.destination.PaneID, "#{pane_index}")
				return func() {
					f.run(t, "kill-pane", "-t", f.destination.PaneID)
					require.Equal(t, index, f.run(t, "display-message", "-p", "-t", other, "#{pane_index}"))
				}
			},
			wantErr: "can't find pane",
		},
		{
			name: "window moved to another session",
			prepare: func(t *testing.T, f navigationFixture) func() {
				t.Helper()
				other := f.run(t, "new-session", "-d", "-s", "other", "-P", "-F", "#{window_id}", "/bin/sh", "-c", "exec sleep 60")
				return func() { f.run(t, "swap-window", "-d", "-s", f.destination.WindowID, "-t", other) }
			},
			wantErr: "can't find window",
		},
		{
			name: "pane moved to another window",
			prepare: func(t *testing.T, f navigationFixture) func() {
				t.Helper()
				_, other := f.newWindow(t, f.destination.SessionID)
				return func() { f.run(t, "swap-pane", "-d", "-s", f.destination.PaneID, "-t", other) }
			},
			wantErr: "can't find pane",
		},
		{
			name: "linked occurrence removed while another link survives",
			prepare: func(t *testing.T, f navigationFixture) func() {
				t.Helper()
				other := f.run(t, "new-session", "-d", "-s", "other", "-P", "-F", "#{session_id}", "/bin/sh", "-c", "exec sleep 60")
				f.run(t, "link-window", "-s", f.destination.WindowID, "-t", other+":4")
				f.newWindow(t, f.destination.SessionID)
				return func() { f.run(t, "unlink-window", "-t", f.destination.SessionID+":"+f.destination.WindowID) }
			},
			wantErr: "can't find window",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newNavigationFixture(t)
			owner := fixture.attach(t, fixture.source.SessionID)
			_, observer := fixture.observe(t)
			before := test.prepare(t, fixture)
			fixture.backend.client = navigationDispatchClient{Client: fixture.backend.client, before: before}

			err := fixture.backend.Switch(fixture.ctx, fixture.destination)

			require.ErrorContains(t, err, test.wantErr)
			assert.Equal(t, fixture.source, fixture.clientTarget(t, owner))
			assert.Equal(t, fixture.source, fixture.clientTarget(t, observer))
		})
	}
}
