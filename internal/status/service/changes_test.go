package service_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestChangesPublishDeadlineExtensionsTogether(t *testing.T) {
	first := fixturePane(service.Screen)
	second := fixturePane(service.Screen)
	second.Ref.ID = "%2"
	core := fixtureService(t, first, second)
	targets := []service.Target{
		targetFor(t, core, first, service.Screen),
		targetFor(t, core, second, service.Screen),
	}

	value := status.Value{State: status.Working, ValidUntil: time.Now().Add(10 * time.Second)}
	for _, target := range targets {
		require.NoError(t, core.Apply(target, 1, value))
	}

	before, changes := core.Changes()
	value.ValidUntil = value.ValidUntil.Add(time.Second)
	for _, target := range targets {
		require.NoError(t, core.Apply(target, 2, value))
	}
	unpublished, _ := core.Changes()
	require.Equal(t, before, unpublished)

	core.PublishRenewals()
	core.PublishRenewals()

	after, _ := core.Changes()
	require.Equal(t, before+1, after, "extensions coalesce and repeat publication is a no-op")
	snapshot, err := core.ReadStatus(t.Context())
	require.NoError(t, err)
	for _, target := range targets {
		require.Equal(t, value.ValidUntil, snapshot.Lookup(target.Pane.Ref).ValidUntil)
	}
	select {
	case <-changes:
	default:
		t.Fatal("deadline publication did not wake readers")
	}
}

func TestChangesWakeOnActivityInvalidationAndClose(t *testing.T) {
	pane := fixturePane(service.Protocol)
	core := fixtureService(t, pane)
	target := targetFor(t, core, pane, service.Protocol)
	_, changes := core.Changes()

	require.NoError(t, core.Apply(target, 1, status.Value{State: status.Blocked}))

	select {
	case <-changes:
	default:
		t.Fatal("activity did not wake readers")
	}
	_, changes = core.Changes()
	require.NoError(t, core.Invalidate(2))
	select {
	case <-changes:
	default:
		t.Fatal("invalidation did not wake readers")
	}
	_, changes = core.Changes()
	require.NoError(t, core.Close())
	select {
	case <-changes:
	default:
		t.Fatal("close did not wake readers")
	}
}

func TestExpiredScreenEvidenceRecoveryWakesReaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pane := fixturePane(service.Screen)
		core := fixtureService(t, pane)
		target := targetFor(t, core, pane, service.Screen)
		require.NoError(t, core.Apply(target, 1, status.Value{State: status.Idle, ValidUntil: time.Now().Add(time.Second)}))
		before, changes := core.Changes()

		time.Sleep(2 * time.Second)
		require.NoError(t, core.Apply(target, 2, status.Value{State: status.Idle, ValidUntil: time.Now().Add(time.Second)}))

		after, _ := core.Changes()
		require.Greater(t, after, before)
		select {
		case <-changes:
		default:
			t.Fatal("expired activity recovery did not wake readers")
		}
	})
}

func TestShortenedScreenDeadlineWakesReaders(t *testing.T) {
	pane := fixturePane(service.Screen)
	core := fixtureService(t, pane)
	target := targetFor(t, core, pane, service.Screen)
	deadline := time.Now().Add(5 * time.Second)
	require.NoError(t, core.Apply(target, 1, status.Value{State: status.Idle, ValidUntil: deadline}))
	_, changes := core.Changes()

	require.NoError(t, core.Apply(target, 2, status.Value{State: status.Idle, ValidUntil: deadline.Add(-time.Second)}))

	select {
	case <-changes:
	default:
		t.Fatal("shortened evidence deadline did not wake readers")
	}
}
