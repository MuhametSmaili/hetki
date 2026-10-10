package statusobserve

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFailedSubscriptionBacksOffDespiteNewInventories(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		failure := errors.New("temporarily unavailable")
		streams := newStreams(t.Context(), "server", &recorder{}, func(context.Context, string) (connection, error) {
			calls++
			return nil, failure
		})
		defer func() { require.NoError(t, streams.Close()) }()
		require.ErrorIs(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")), failure)

		for revision := uint64(2); revision < 10; revision++ {
			require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(revision, "$1")))
		}
		require.Equal(t, 1, calls)
		time.Sleep(time.Until(streams.nextRetry()))
		err := streams.reconcile(t.Context(), fixtureTopology(10, "$1"))

		require.ErrorIs(t, err, failure)
		require.Equal(t, 2, calls)
		require.GreaterOrEqual(t, time.Until(streams.nextRetry()), 800*time.Millisecond)
	})
}

func TestRetryHistorySurvivesShortConnectionsButResetsAfterHealthyInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		streams, _, connections := streamFixture(t)
		require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
		connections["$1"].failure <- errors.New("first disconnect")
		await(t, streams.Changes())
		time.Sleep(time.Until(streams.nextRetry()))
		require.Error(t, streams.reconcile(t.Context(), fixtureTopology(2, "$1")))

		connections["$1"].failure <- errors.New("short-lived replacement")
		await(t, streams.Changes())
		require.GreaterOrEqual(t, time.Until(streams.nextRetry()), 800*time.Millisecond)
		time.Sleep(time.Until(streams.nextRetry()))
		require.Error(t, streams.reconcile(t.Context(), fixtureTopology(3, "$1")))
		time.Sleep(healthyInterval)
		connections["$1"].failure <- errors.New("healthy replacement ended")
		await(t, streams.Changes())

		require.GreaterOrEqual(t, time.Until(streams.nextRetry()), 400*time.Millisecond)
		require.LessOrEqual(t, time.Until(streams.nextRetry()), 500*time.Millisecond)
	})
}

func TestInvalidationDisablesInputUntilFreshInventory(t *testing.T) {
	streams, record, connections := streamFixture(t)
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
	connections["$1"].send(t, "%1", "before")
	before, resetsBefore := record.snapshot()

	streams.invalidate()
	connections["$1"].send(t, "%1", "unvalidated")
	afterFailure, resetsAfter := record.snapshot()
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(2, "$1")))
	connections["$1"].send(t, "%1", "fresh")
	after, _ := record.snapshot()

	require.Len(t, afterFailure, 1)
	require.Len(t, resetsAfter, len(resetsBefore)+1)
	require.Len(t, after, 2)
	require.Equal(t, before[0].scope.Connection, after[1].scope.Connection)
	require.Greater(t, after[1].scope.Generation, before[0].scope.Generation)
}
