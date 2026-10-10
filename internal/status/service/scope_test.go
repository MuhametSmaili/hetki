package service_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	statusservice "github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestScopeHandoffRevokesOnlyAffectedPane(t *testing.T) {
	pane, peer := fixturePane(""), fixturePane("")
	peer.Ref.ID = "%2"
	core := fixtureService(t, pane, peer)
	old := targetFor(t, core, pane, statusservice.OSC7501)
	other := targetFor(t, core, peer, statusservice.OSC7501)
	require.NoError(t, core.Apply(old, 1, status.Value{State: status.Working}))
	require.NoError(t, core.Apply(other, 1, status.Value{State: status.Idle}))
	changed := pane.Scope
	changed.Connection = "replacement"

	require.NoError(t, core.UpdateScope(2, pane.Ref, changed))
	require.NoError(t, core.UpdateScope(3, pane.Ref, pane.Scope))
	err := core.Apply(old, 2, status.Value{State: status.Idle})
	snapshot, readErr := core.ReadStatus(t.Context())

	require.ErrorIs(t, err, statusservice.ErrStale)
	require.NoError(t, readErr)
	require.Equal(t, status.Unknown, snapshot.At(pane.Ref, time.Now()))
	require.Equal(t, status.Idle, snapshot.At(peer.Ref, time.Now()))
	require.NoError(t, core.Apply(other, 2, status.Value{State: status.Blocked}))
	require.ErrorIs(t, core.UpdateInventory(inventory(2, pane, peer)), statusservice.ErrStale)
}

func TestScopeUpdateCannotExtendInventoryValidation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pane := fixturePane("")
		core := fixtureService(t, pane)
		time.Sleep(59 * time.Second)
		pane.Scope.Connection = "replacement"

		require.NoError(t, core.UpdateScope(2, pane.Ref, pane.Scope))
		target := targetFor(t, core, pane, statusservice.OSC7501)
		time.Sleep(time.Second)
		err := core.Apply(target, 1, status.Value{State: status.Working})

		require.ErrorIs(t, err, statusservice.ErrStale)
		_, available := core.Target(pane.Ref, statusservice.OSC7501)
		require.False(t, available)
	})
}
