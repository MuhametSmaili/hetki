package statusobserve

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status/service"
)

const (
	recoveryInterval = 2 * time.Second
	hintDelay        = 50 * time.Millisecond
)

// Watch runs one synchronous acquisition loop until cancellation or a
// terminal failure. The caller owns Streams.Close; Watch exclusively
// owns reconciliation while running. It invalidates routes on acquisition failure
// and before returning. Stream consumers still receive output concurrently.
//
// publish receives each acquired inventory and any acquisition/reconciliation
// error for service updates and diagnostics. An acquisition failure has a zero
// inventory. publish must be bounded; its own error terminates the watch. Optional
// screens share this loop and timer; due inventory takes priority over a batch.
func Watch(ctx context.Context, sampler *observe.Sampler, streams *Streams, screens *Screens, publish func(observe.Inventory, error) error) (err error) {
	if sampler == nil || streams == nil || publish == nil {
		return fmt.Errorf("inventory watch requires sampler, streams and publisher")
	}

	if !streams.watching.CompareAndSwap(false, true) {
		return fmt.Errorf("inventory watch already running")
	}
	defer streams.watching.Store(false)
	if screens != nil {
		if !screens.watching.CompareAndSwap(false, true) {
			return fmt.Errorf("screen scheduler already running")
		}
		defer screens.watching.Store(false)
	}
	defer streams.invalidate()
	defer func() { err = errors.Join(err, screens.clear()) }()

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
		screens:   screens,
		refresh: func(ctx context.Context) (bool, error) {
			revision++
			return refreshInventory(ctx, sampler.Inventory, streams, screens, revision, publish)
		},
	}
	return loop.run(ctx)
}

func refreshInventory(ctx context.Context, acquire func(context.Context, uint64) (observe.Inventory, error), streams *Streams, screens *Screens, revision uint64, publish func(observe.Inventory, error) error) (bool, error) {
	inventory, err := acquire(ctx, revision)
	failed := err != nil
	if failed {
		streams.invalidate()
		err = errors.Join(err, screens.clear())
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
	screens   *Screens
}

type inventorySchedule struct {
	due      time.Time
	floor    time.Time
	failures uint8
}

func (l inventoryLoop) run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()

	schedule := inventorySchedule{due: time.Now()}
	hints := l.changes
	for {
		inventoryAt := later(earlier(schedule.due, l.nextRetry()), schedule.floor)
		wakeAt := earlier(inventoryAt, l.screens.nextCapture())
		timer.Reset(max(0, time.Until(wakeAt)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-hints:
			schedule.due = earlier(schedule.due, time.Now().Add(hintDelay))
			hints = nil
		case <-l.screens.changes():
		case <-timer.C:
			refreshed, err := l.step(ctx, inventoryAt, &schedule)
			if err != nil {
				return err
			}
			if refreshed {
				hints = l.changes
			}
		}
	}
}

func (l inventoryLoop) step(ctx context.Context, inventoryAt time.Time, schedule *inventorySchedule) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if time.Now().Before(inventoryAt) {
		return false, l.captureScreens(ctx)
	}

	// Hints already queued are covered by this acquisition. Later hints survive.
	select {
	case <-l.changes:
	default:
	}
	failed, err := l.refresh(ctx)
	if err != nil {
		return false, err
	}
	schedule.due, schedule.floor, schedule.failures = nextRefresh(failed, schedule.failures)
	return true, nil
}

func (l inventoryLoop) captureScreens(ctx context.Context) error {
	err := l.screens.runBatch(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		l.screens.report(err)
	}
	if errors.Is(err, service.ErrClosed) || errors.Is(err, observe.ErrServerChanged) {
		return err
	}
	return nil
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

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
