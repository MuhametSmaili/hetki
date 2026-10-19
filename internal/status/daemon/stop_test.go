package daemon

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func gatedCleanup(t *testing.T, name string) (*Client, <-chan struct{}, func(), <-chan error) {
	t.Helper()
	client := fixtureClient(t, name)
	closing := make(chan struct{})
	finishClose := make(chan struct{})
	release := sync.OnceFunc(func() { close(finishClose) })
	// Register after startup's join cleanup, even if readiness calls Fatal.
	defer t.Cleanup(release)

	done := startFixture(t, client, liveBinding(client.server), func(context.Context, func(error)) (Source, error) {
		return fixtureSource{
			read: func(context.Context) (status.Snapshot, error) { return status.Snapshot{}, nil },
			close: func() error {
				close(closing)
				<-finishClose
				return nil
			},
		}, nil
	})
	return client, closing, release, done
}

func TestEnsureDoesNotLaunchDuringSourceCleanup(t *testing.T) {
	client, closing, _, _ := gatedCleanup(t, "start-during-cleanup")
	info, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.NoError(t, client.requestStop(t.Context(), info.Epoch))
	receive(t, closing)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	prepared := false

	_, err = Ensure(ctx, client.server, "", func(context.Context) ([]string, error) {
		prepared = true
		return nil, errors.New("must not prepare before cleanup")
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, prepared)
}

func TestEnsureStartsAfterSourceCleanup(t *testing.T) {
	client, closing, release, done := gatedCleanup(t, "start-after-cleanup")
	t.Cleanup(func() {
		release()
		stopProcessFixture(t, client)
	})
	before, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.NoError(t, client.requestStop(t.Context(), before.Epoch))
	receive(t, closing)
	started := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	go func() {
		_, err := Ensure(ctx, client.server, "", fixtureArguments(client.server))
		started <- err
	}()
	release()

	require.NoError(t, receive(t, started))
	require.NoError(t, receive(t, done))
	after, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, before.Epoch, after.Epoch)
}

func TestStopWaitsForSourceCleanup(t *testing.T) {
	client, closing, _, _ := gatedCleanup(t, "stop-cleanup")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	err := client.Stop(ctx)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	receive(t, closing)
}

func TestCanceledStopDoesNotUndoAcceptedShutdown(t *testing.T) {
	client, closing, release, done := gatedCleanup(t, "canceled-stop")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- client.Stop(ctx) }()
	receive(t, closing)

	cancel()

	require.ErrorIs(t, receive(t, stopped), context.Canceled)
	owned, err := endpointOwned(client.path)
	require.NoError(t, err)
	require.True(t, owned, "cancellation must not release another process's lock")
	release()
	require.NoError(t, receive(t, done))
}

func TestStopDoesNotInferAnInstanceFromBusyLock(t *testing.T) {
	client, closing, _, _ := gatedCleanup(t, "stop-without-listener")
	info, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.NoError(t, client.requestStop(t.Context(), info.Epoch))
	receive(t, closing)

	stopped, err := Stop(t.Context(), client.server)

	require.ErrorIs(t, err, ErrTransition)
	require.Nil(t, stopped)
}

func TestStopRequestRequiresTargetBeforeConnecting(t *testing.T) {
	for _, epoch := range []string{"", "invalid"} {
		err := (&Client{}).requestStop(t.Context(), epoch)

		require.ErrorIs(t, err, ErrIdentity)
	}
}

func TestStopRejectsMissingMalformedAndChangedTargets(t *testing.T) {
	client := fixtureClient(t, "stop-target")
	startFixture(t, client, liveBinding(client.server), staticSource(status.Snapshot{}))
	before, err := client.Ping(t.Context())
	require.NoError(t, err)

	for _, tc := range []struct{ name, epoch string }{
		{"missing", ""}, {"malformed", "bad"}, {"changed", strings.Repeat("a", 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.callRequest(t.Context(), request{
				Version: protocolVersion, Server: client.server, Action: "stop", ExpectedEpoch: tc.epoch,
			}, requestTimeout)
			after, pingErr := client.Ping(t.Context())

			require.Error(t, err)
			require.NoError(t, pingErr)
			require.Equal(t, before, after)
		})
	}
}

func TestStopRefusesUnconfirmedLockOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, owner string
		want        error
	}{
		{"legacy empty record", "", ErrLifecycle},
		{"malformed record", "bad", ErrLifecycle},
		{"changed owner", strings.Repeat("a", 32), ErrIdentity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, "unconfirmed-owner")
			startFixture(t, client, liveBinding(client.server), staticSource(status.Snapshot{}))
			before, err := client.Ping(t.Context())
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(client.path+".lock", []byte(tc.owner), 0600))

			err = client.Stop(t.Context())
			after, pingErr := client.Ping(t.Context())

			require.ErrorIs(t, err, tc.want)
			require.NoError(t, pingErr)
			require.Equal(t, before, after)
		})
	}
}

func TestStopRefusesUnsafeLockWithoutChangingDaemon(t *testing.T) {
	client := fixtureClient(t, "unsafe-stop-lock")
	startFixture(t, client, liveBinding(client.server), staticSource(status.Snapshot{}))
	before, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(client.path+".lock", 0644))

	err = client.Stop(t.Context())
	after, pingErr := client.Ping(t.Context())

	require.ErrorContains(t, err, "unsafe status daemon lock")
	require.NoError(t, pingErr)
	require.Equal(t, before, after)
}

func TestStopNeverFollowsReplacement(t *testing.T) {
	client := fixtureClient(t, "stop-replacement")
	done := startFixture(t, client, liveBinding(client.server), staticSource(status.Snapshot{}))
	before, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.NoError(t, client.Stop(t.Context()))
	require.NoError(t, receive(t, done))
	startFixture(t, client, liveBinding(client.server), staticSource(status.Snapshot{}))
	after, err := client.Ping(t.Context())
	require.NoError(t, err)

	err = client.stopObserved(t.Context(), before)

	require.ErrorIs(t, err, ErrIdentity)
	running, err := client.Ping(t.Context())
	require.NoError(t, err)
	require.Equal(t, after, running)
}
