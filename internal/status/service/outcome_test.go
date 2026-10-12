package service_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestOutcomeRenewsUntilReplacedOrInvalidated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pane := fixturePane(service.Protocol)
		core := fixtureService(t, pane)
		target := targetFor(t, core, pane, service.Protocol)
		require.NoError(t, core.Apply(target, 1, status.Value{State: status.Unknown, Outcome: status.Completed}))
		before, err := core.ReadStatus(t.Context())
		require.NoError(t, err)

		for revision := uint64(2); revision <= 5; revision++ {
			time.Sleep(30 * time.Second)
			require.NoError(t, core.UpdateInventory(inventory(revision, pane)))
		}
		after, err := core.ReadStatus(t.Context())

		require.NoError(t, err)
		require.Equal(t, status.NoOutcome, before.Lookup(pane.Ref).OutcomeAt(time.Now()), "old client snapshots still expire")
		require.Equal(t, status.Completed, after.Lookup(pane.Ref).OutcomeAt(time.Now()))
		require.Equal(t, status.Unknown, after.At(pane.Ref, time.Now()))
		require.NoError(t, core.Apply(target, 2, status.Value{State: status.Working}))
		after, err = core.ReadStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, status.NoOutcome, after.Lookup(pane.Ref).Outcome)
		require.NoError(t, core.Apply(target, 3, status.Value{State: status.Unknown, Outcome: status.Failed}))
		require.NoError(t, core.Invalidate(6))
		after, err = core.ReadStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, status.NoOutcome, after.Lookup(pane.Ref).Outcome)
	})
}

func TestOutcomeChangeWakesReadersWithoutActivityChange(t *testing.T) {
	pane := fixturePane(service.Protocol)
	core := fixtureService(t, pane)
	target := targetFor(t, core, pane, service.Protocol)
	for i, outcome := range []status.Outcome{status.Completed, status.Failed, status.NoOutcome} {
		_, changes := core.Changes()

		require.NoError(t, core.Apply(target, uint64(i+1), status.Value{State: status.Unknown, Outcome: outcome}))

		select {
		case <-changes:
		default:
			t.Fatal("outcome-only change did not wake readers")
		}
	}
}

func TestScreenCannotSupplyUnvalidatedOutcome(t *testing.T) {
	pane := fixturePane(service.Screen)
	core := fixtureService(t, pane)
	target := targetFor(t, core, pane, service.Screen)

	err := core.Apply(target, 1, status.Value{State: status.Unknown, Outcome: status.Completed})

	require.Error(t, err)
}
