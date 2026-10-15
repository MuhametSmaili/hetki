package statusobserver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func startLoop(t *testing.T, loop inventoryLoop) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Error("inventory loop did not stop")
		}
	})
	return cancel, done
}

func noRetry() time.Time { return time.Time{} }

type refreshLog struct {
	mu    sync.Mutex
	times []time.Time
}

func (l *refreshLog) record() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.times = append(l.times, time.Now())
}

func (l *refreshLog) snapshot() []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Time(nil), l.times...)
}

func hint(changes chan<- struct{}) {
	select {
	case changes <- struct{}{}:
	default:
	}
}

func TestInventoryWatchCoalescesHintsWithoutPostponingRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		changes := make(chan struct{}, 1)
		var calls atomic.Int64
		_, _ = startLoop(t, inventoryLoop{
			changes: changes, nextRetry: noRetry,
			refresh: func(context.Context) (bool, error) {
				calls.Add(1)
				return false, nil
			},
		})
		synctest.Wait()
		require.Equal(t, int64(1), calls.Load())

		for range 5 {
			for range 1000 {
				hint(changes)
			}
			synctest.Wait()
			time.Sleep(10 * time.Millisecond)
		}
		synctest.Wait()

		require.Equal(t, int64(2), calls.Load())
		time.Sleep(recoveryInterval)
		synctest.Wait()
		require.Equal(t, int64(3), calls.Load())
	})
}

func TestInventoryWatchRetainsHintsDuringSingleFlightAcquisition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		changes := make(chan struct{}, 1)
		release := make(chan struct{})
		var calls atomic.Int64
		_, _ = startLoop(t, inventoryLoop{
			changes: changes, nextRetry: noRetry,
			refresh: func(ctx context.Context) (bool, error) {
				if calls.Add(1) == 1 {
					select {
					case <-release:
					case <-ctx.Done():
						return false, ctx.Err()
					}
				}
				return false, nil
			},
		})
		synctest.Wait()

		for range 1000 {
			hint(changes)
		}
		time.Sleep(recoveryInterval)
		synctest.Wait()
		require.Equal(t, int64(1), calls.Load())
		close(release)
		synctest.Wait()
		time.Sleep(hintDelay)
		synctest.Wait()

		require.Equal(t, int64(2), calls.Load())
	})
}

func TestInventoryWatchFailureBackoffResistsHintsAndIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		changes := make(chan struct{}, 1)
		var log refreshLog
		_, _ = startLoop(t, inventoryLoop{
			changes: changes, nextRetry: noRetry,
			refresh: func(context.Context) (bool, error) {
				log.record()
				return true, nil
			},
		})
		synctest.Wait()

		for range 30 {
			hint(changes)
			synctest.Wait()
			time.Sleep(10 * time.Millisecond)
		}
		synctest.Wait()
		require.Len(t, log.snapshot(), 1)
		time.Sleep(8 * time.Second)
		synctest.Wait()

		times := log.snapshot()
		require.GreaterOrEqual(t, len(times), 5)
		for n := 1; n < len(times); n++ {
			ceiling := min(500*time.Millisecond<<min(n-1, 2), recoveryInterval)
			delay := times[n].Sub(times[n-1])
			require.GreaterOrEqual(t, delay, ceiling*4/5)
			require.LessOrEqual(t, delay, ceiling)
		}
	})
}

func TestInventoryWatchUsesRetryDeadlineThenPeriodicRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64
		retryAt := time.Now().Add(300 * time.Millisecond)
		_, _ = startLoop(t, inventoryLoop{
			nextRetry: func() time.Time {
				if calls.Load() < 2 {
					return retryAt
				}
				return time.Time{}
			},
			refresh: func(context.Context) (bool, error) {
				calls.Add(1)
				return false, nil
			},
		})
		synctest.Wait()

		time.Sleep(300 * time.Millisecond)
		synctest.Wait()
		require.Equal(t, int64(2), calls.Load())
		time.Sleep(recoveryInterval)
		synctest.Wait()

		require.Equal(t, int64(3), calls.Load())
	})
}

func TestInventoryWatchPropagatesTerminalFailure(t *testing.T) {
	failure := errors.New("publisher failed")
	loop := inventoryLoop{
		nextRetry: noRetry,
		refresh:   func(context.Context) (bool, error) { return false, failure },
	}

	err := loop.run(t.Context())

	require.ErrorIs(t, err, failure)
}

func TestInventoryWatchCancelsInFlightAcquisition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		finished := make(chan struct{})
		cancel, _ := startLoop(t, inventoryLoop{
			nextRetry: noRetry,
			refresh: func(ctx context.Context) (bool, error) {
				defer close(finished)
				<-ctx.Done()
				return false, ctx.Err()
			},
		})
		synctest.Wait()

		cancel()
		await(t, finished)
	})
}

func TestInventoryAcquisitionFailureRevokesInputAndPreservesCause(t *testing.T) {
	cases := []struct {
		name     string
		failure  error
		terminal bool
	}{
		{"transient query failure", errors.New("query unavailable"), false},
		{"server replacement", observe.ErrServerChanged, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			streams, record, connections := streamFixture(t)
			require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
			connections["$1"].send(t, "%1", "before")
			acquire := func(_ context.Context, revision uint64) (observe.Inventory, error) {
				require.Equal(t, uint64(2), revision)
				return observe.Inventory{}, tc.failure
			}
			var reported error

			failed, err := refreshInventory(t.Context(), acquire, streams, nil, 2, nil, func(inventory observe.Inventory, err error) error {
				require.Zero(t, inventory.Revision())
				reported = err
				return nil
			})
			connections["$1"].send(t, "%1", "unvalidated")
			outputs, _ := record.snapshot()

			require.ErrorIs(t, reported, tc.failure)
			require.Len(t, outputs, 1)
			if tc.terminal {
				require.ErrorIs(t, err, tc.failure)
			} else {
				require.NoError(t, err)
				require.True(t, failed)
			}
		})
	}
}
