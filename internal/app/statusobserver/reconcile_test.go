package statusobserver

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func TestRemovedSessionIsJoinedAndPaneIDReuseGetsNewScope(t *testing.T) {
	streams, record, connections := streamFixture(t)
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
	old := connections["$1"]
	old.send(t, "%1", "old")
	before, _ := record.snapshot()
	empty := fixtureTopology(2)
	delete(empty.panes, "%1")

	err := streams.reconcile(t.Context(), empty)
	await(t, old.joined)
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(3, "$1")))
	connections["$1"].send(t, "%1", "replacement")
	after, _ := record.snapshot()

	require.NoError(t, err)
	require.Len(t, after, 2)
	require.Greater(t, after[1].scope.Generation, before[0].scope.Generation)
	select {
	case <-streams.Changes():
		t.Fatal("intentional retirement must not trigger recovery")
	default:
	}
}

func TestSubscriptionLimitRetainsExistingOwners(t *testing.T) {
	streams, record, connections := streamFixture(t)
	sessions := make([]string, maxSubscriptions)
	for n := range sessions {
		sessions[n] = fmt.Sprintf("$%03d", n+1)
	}
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, sessions...)))
	connections["$001"].send(t, "%1", "before")
	before, resetsBefore := record.snapshot()
	sessions = append(sessions, "$000")

	err := streams.reconcile(t.Context(), fixtureTopology(2, sessions...))
	connections["$001"].send(t, "%1", "after")
	after, resetsAfter := record.snapshot()
	closeErr := streams.Close()

	require.ErrorIs(t, err, ErrSubscriptionLimit)
	require.Len(t, connections, maxSubscriptions)
	require.Nil(t, connections["$000"])
	require.Equal(t, before[0].scope, after[1].scope)
	require.Equal(t, resetsBefore, resetsAfter)
	require.NoError(t, closeErr)
	for _, conn := range connections {
		await(t, conn.joined)
	}
}

func TestReconcileRejectsOldOrForeignInventoryWithoutRevokingRoutes(t *testing.T) {
	streams, record, connections := streamFixture(t)
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(2, "$1")))
	connections["$1"].send(t, "%1", "before")
	before, resetsBefore := record.snapshot()
	foreign := fixtureTopology(3, "$2")
	foreign.server = "replacement"

	staleErr := streams.reconcile(t.Context(), fixtureTopology(1, "$2"))
	foreignErr := streams.reconcile(t.Context(), foreign)
	connections["$1"].send(t, "%1", "after")
	after, resetsAfter := record.snapshot()

	require.ErrorIs(t, staleErr, ErrStaleInventory)
	require.ErrorIs(t, foreignErr, observe.ErrServerChanged)
	require.Len(t, connections, 1)
	require.Equal(t, before[0].scope, after[1].scope)
	require.Equal(t, resetsBefore, resetsAfter)
}

func TestPartialStartupRemainsUsableAndRetriesWithFreshInventory(t *testing.T) {
	synctest.Test(t, testPartialStartupRemainsUsableAndRetriesWithFreshInventory)
}

func testPartialStartupRemainsUsableAndRetriesWithFreshInventory(t *testing.T) {
	record := &recorder{}
	failed := errors.New("session temporarily unavailable")
	first, second := fakeStream("first"), fakeStream("second")
	retry := false
	streams := newStreams(t.Context(), "server", record, func(_ context.Context, session string) (connection, error) {
		if session == "$1" {
			return first, nil
		}
		if !retry {
			return nil, failed
		}
		return second, nil
	})
	defer func() { require.NoError(t, streams.Close()) }()

	initialErr := streams.reconcile(t.Context(), fixtureTopology(1, "$1", "$2"))
	first.send(t, "%1", "still usable")
	retry = true
	time.Sleep(time.Until(streams.nextRetry()))
	retryErr := streams.reconcile(t.Context(), fixtureTopology(2, "$1", "$2"))
	second.send(t, "%1", "linked duplicate")
	outputs, _ := record.snapshot()

	require.ErrorIs(t, initialErr, failed)
	require.NoError(t, retryErr)
	require.Len(t, outputs, 1)
	require.Equal(t, "still usable", outputs[0].text)
}
