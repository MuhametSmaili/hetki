package service_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/service"
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

func TestOutcomeObservationSurvivesRepeatsAndRestartsOnNewEvidence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pane := fixturePane(service.Protocol)
		core := fixtureService(t, pane)
		target := targetFor(t, core, pane, service.Protocol)
		started := time.Now()
		success := status.Value{State: status.Unknown, Outcome: status.Completed, OutcomeSince: started}
		require.NoError(t, core.Apply(target, 1, success))
		_, changes := core.Changes()

		time.Sleep(3 * time.Second)
		require.NoError(t, core.Apply(target, 2, success))
		snapshot, err := core.ReadStatus(t.Context())

		require.NoError(t, err)
		require.Equal(t, started, snapshot.Lookup(pane.Ref).OutcomeSince)
		select {
		case <-changes:
			t.Fatal("repeated success restarted its observation")
		default:
		}

		time.Sleep(3 * time.Second)
		require.NoError(t, core.UpdateInventory(inventory(2, pane)))
		snapshot, err = core.ReadStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, started, snapshot.Lookup(pane.Ref).OutcomeSince)

		require.NoError(t, core.Apply(target, 3, status.Value{State: status.Working}))
		snapshot, err = core.ReadStatus(t.Context())
		require.NoError(t, err)
		require.True(t, snapshot.Lookup(pane.Ref).OutcomeSince.IsZero())
		success.OutcomeSince = time.Now()
		require.NoError(t, core.Apply(target, 4, success))
		snapshot, err = core.ReadStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, time.Now(), snapshot.Lookup(pane.Ref).OutcomeSince)
		require.NotEqual(t, started, snapshot.Lookup(pane.Ref).OutcomeSince)

		require.NoError(t, core.Invalidate(3))
		snapshot, err = core.ReadStatus(t.Context())
		require.NoError(t, err)
		require.True(t, snapshot.Lookup(pane.Ref).OutcomeSince.IsZero())
	})
}

func TestNewOutcomeObservationWakesReaders(t *testing.T) {
	pane := fixturePane(service.Protocol)
	core := fixtureService(t, pane)
	target := targetFor(t, core, pane, service.Protocol)
	value := status.Value{State: status.Unknown, Outcome: status.Completed, OutcomeSince: time.Now().Add(-time.Minute)}
	require.NoError(t, core.Apply(target, 1, value))
	_, changes := core.Changes()
	value.OutcomeSince = time.Now()

	require.NoError(t, core.Apply(target, 2, value))

	select {
	case <-changes:
	default:
		t.Fatal("new observation of the same outcome did not wake readers")
	}
}

func TestOutcomeLifetimeChangeWakesReadersAndSurvivesRenewal(t *testing.T) {
	pane := fixturePane(service.Protocol)
	core := fixtureService(t, pane)
	target := targetFor(t, core, pane, service.Protocol)
	value := status.Value{State: status.Unknown, Outcome: status.Completed, OutcomeSince: time.Now()}
	require.NoError(t, core.Apply(target, 1, value))
	_, changes := core.Changes()
	value.OutcomeEnded = true

	require.NoError(t, core.Apply(target, 2, value))

	select {
	case <-changes:
	default:
		t.Fatal("ending foreground attribution did not wake readers")
	}
	_, repeated := core.Changes()
	require.NoError(t, core.Apply(target, 3, value))
	select {
	case <-repeated:
		t.Fatal("identical ended outcome woke readers")
	default:
	}
	require.NoError(t, core.UpdateInventory(inventory(2, pane)))
	after, err := core.ReadStatus(t.Context())
	require.NoError(t, err)
	require.True(t, after.Lookup(pane.Ref).OutcomeEnded)
	require.Equal(t, value.OutcomeSince, after.Lookup(pane.Ref).OutcomeSince)
}

func TestEndedOutcomeDoesNotRestoreUnknownStatusAfterContinuityLoss(t *testing.T) {
	cases := []struct {
		name      string
		interrupt func(*testing.T, *service.Service, *service.Pane)
	}{
		{"expired validation", func(_ *testing.T, _ *service.Service, _ *service.Pane) {
			time.Sleep(time.Minute + time.Second)
		}},
		{"failed inventory", func(t *testing.T, core *service.Service, _ *service.Pane) {
			require.NoError(t, core.Invalidate(2))
		}},
		{"new connection", func(_ *testing.T, _ *service.Service, pane *service.Pane) {
			pane.Scope.Connection = "replacement"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pane := fixturePane(service.Protocol)
				core := fixtureService(t, pane)
				target := targetFor(t, core, pane, service.Protocol)
				require.NoError(t, core.Apply(target, 1, status.Value{
					State: status.Unknown, Outcome: status.Completed, OutcomeSince: time.Now(), OutcomeEnded: true,
				}))

				tc.interrupt(t, core, &pane)
				require.NoError(t, core.UpdateInventory(inventory(3, pane)))
				after, err := core.ReadStatus(t.Context())

				require.NoError(t, err)
				require.Equal(t, status.Withdrawn, after.At(pane.Ref, time.Now()))
				require.Equal(t, status.NoOutcome, after.Lookup(pane.Ref).Outcome)
				pane.Scope.Connection = "another-connection"
				require.NoError(t, core.UpdateInventory(inventory(4, pane)))
				after, err = core.ReadStatus(t.Context())
				require.NoError(t, err)
				require.Equal(t, status.Withdrawn, after.At(pane.Ref, time.Now()))
			})
		})
	}
}

func TestScreenCannotSupplyUnvalidatedOutcome(t *testing.T) {
	pane := fixturePane(service.Screen)
	core := fixtureService(t, pane)
	target := targetFor(t, core, pane, service.Screen)

	err := core.Apply(target, 1, status.Value{State: status.Unknown, Outcome: status.Completed})

	require.Error(t, err)
}
