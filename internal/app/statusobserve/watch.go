package statusobserve

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
)

const (
	recoveryInterval = 2 * time.Second
	hintDelay        = 50 * time.Millisecond
)

// WatchInventory runs one synchronous acquisition loop until cancellation or a
// terminal failure. The caller owns Streams.Close; WatchInventory exclusively
// owns reconciliation while running. It invalidates routes on acquisition failure
// and before returning. Stream consumers still receive output concurrently.
//
// publish receives each acquired inventory and any acquisition/reconciliation
// error for service updates and diagnostics. An acquisition failure has a zero
// inventory. publish must be bounded; its own error terminates the watch. No
// screen capture or status inference runs here.
func WatchInventory(ctx context.Context, sampler *observe.Sampler, streams *Streams, publish func(observe.Inventory, error) error) error {
	if sampler == nil || streams == nil || publish == nil {
		return fmt.Errorf("inventory watch requires sampler, streams and publisher")
	}

	if !streams.watching.CompareAndSwap(false, true) {
		return fmt.Errorf("inventory watch already running")
	}
	defer streams.watching.Store(false)
	defer streams.invalidate()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stop := context.AfterFunc(streams.ctx, cancel)
	defer stop()

	streams.mu.Lock()
	revision := streams.revision
	streams.mu.Unlock()

	loop := inventoryLoop{
		changes:   streams.Changes(),
		nextRetry: streams.nextRetry,
		refresh: func(ctx context.Context) (bool, error) {
			revision++
			return refreshInventory(ctx, sampler.Inventory, streams, revision, publish)
		},
	}
	return loop.run(ctx)
}

func refreshInventory(ctx context.Context, acquire func(context.Context, uint64) (observe.Inventory, error), streams *Streams, revision uint64, publish func(observe.Inventory, error) error) (bool, error) {
	inventory, err := acquire(ctx, revision)
	failed := err != nil
	if failed {
		streams.invalidate()
	} else {
		err = streams.Reconcile(ctx, inventory)
	}

	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if publishErr := publish(inventory, err); publishErr != nil {
		return false, errors.Join(err, fmt.Errorf("publish status inventory: %w", publishErr))
	}

	if errors.Is(err, observe.ErrServerChanged) || errors.Is(err, ErrClosed) || errors.Is(err, ErrStaleInventory) {
		return false, err
	}
	return failed, nil
}

type inventoryLoop struct {
	changes   <-chan struct{}
	nextRetry func() time.Time
	refresh   func(context.Context) (failed bool, err error)
}

func (l inventoryLoop) run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()

	due := time.Now()
	var retryFloor time.Time
	var failures uint8
	hints := l.changes
	for {
		wakeAt := earlier(due, l.nextRetry())
		if wakeAt.Before(retryFloor) {
			wakeAt = retryFloor
		}

		timer.Reset(max(0, time.Until(wakeAt)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-hints:
			due = earlier(due, time.Now().Add(hintDelay))
			hints = nil
		case <-timer.C:
			if err := ctx.Err(); err != nil {
				return err
			}

			// Hints already queued are covered by the acquisition starting now.
			// Hints arriving during I/O remain queued for a follow-up pass.
			select {
			case <-l.changes:
			default:
			}

			failed, err := l.refresh(ctx)
			if err != nil {
				return err
			}
			due, retryFloor, failures = nextRefresh(failed, failures)
			hints = l.changes
		}
	}
}

func nextRefresh(failed bool, failures uint8) (time.Time, time.Time, uint8) {
	if !failed {
		return time.Now().Add(recoveryInterval), time.Time{}, 0
	}

	failures = min(failures+1, 3)
	after := time.Now().Add(retryDelay(failures))
	return after, after, failures
}

func earlier(a, b time.Time) time.Time {
	if b.IsZero() || !a.IsZero() && a.Before(b) {
		return a
	}
	return b
}
