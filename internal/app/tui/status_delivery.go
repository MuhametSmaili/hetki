package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

// statusReadTimeout is longer than the subscription idle timeout, so the reader
// reports a broken subscription before a UI read times out.
const statusReadTimeout = 40 * time.Second

// statusDelivery owns one picker's received evidence and UI acknowledgements.
// The adapter owns bindings.
type statusDelivery struct {
	ctx      context.Context
	adapter  *LiveAdapter
	readView func(ctx context.Context, after uint64) (daemon.View, uint64, error)
	stop     func()

	mu       sync.Mutex
	view     daemon.View
	sequence uint64
	err      error
	received uint64
	applied  uint64
}

func (d *statusDelivery) options() ui.StatusOptions {
	return ui.StatusOptions{Read: d.readStatus, ReadTimeout: statusReadTimeout}
}

// clear stops the reader and releases retained evidence. The owner cancels ctx
// and joins UI callbacks first.
func (d *statusDelivery) clear() {
	if d.stop != nil {
		d.stop()
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.view, d.err = daemon.View{}, nil
	d.sequence, d.received, d.applied = 0, 0, 0
}

func (d *statusDelivery) readStatus(ctx context.Context, previous status.Snapshot) ui.StatusUpdate {
	if update, ok := d.replayStatus(ctx); ok {
		return update
	}
	if err := ctx.Err(); err != nil {
		return d.receiveStatus(ctx, daemon.View{}, 0, previous, err)
	}

	d.mu.Lock()
	after := d.sequence
	d.mu.Unlock()

	view, sequence, err := d.readView(ctx, after)
	return d.receiveStatus(ctx, view, sequence, previous, err)
}

func (d *statusDelivery) receiveStatus(ctx context.Context, view daemon.View, sequence uint64, previous status.Snapshot, err error) ui.StatusUpdate {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ctx.Err() != nil {
		return ui.StatusUpdate{Values: previous.Invalidate()}
	}
	contextErr := ctx.Err()
	if contextErr != nil {
		err = contextErr
	}
	if err != nil {
		view = d.view
		if d.received == 0 {
			view.Values = previous
		}
		view.Values = view.Values.Invalidate()
	}
	d.view = view
	d.sequence = sequence
	if contextErr != nil {
		// The caller dropped this read, so the next read returns the latest view at once.
		d.sequence = 0
	}
	d.err = nil
	if err != nil && !errors.Is(contextErr, context.Canceled) {
		d.err = fmt.Errorf("status unavailable: %w", err)
	}
	d.received++
	return d.readStatusUpdateLocked()
}

// The sequence records receipt, not UI acceptance. Rebind an unaccepted
// delivery instead of waiting for an unrelated daemon change.
func (d *statusDelivery) replayStatus(ctx context.Context) (ui.StatusUpdate, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ctx.Err() != nil || ctx.Err() != nil || d.received == d.applied {
		return ui.StatusUpdate{}, false
	}
	return d.readStatusUpdateLocked(), true
}

func (d *statusDelivery) acceptStatus(received uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ctx.Err() == nil {
		d.applied = max(d.applied, received)
	}
}

func (d *statusDelivery) projectionStatus() *ui.StatusUpdate {
	d.mu.Lock()
	defer d.mu.Unlock()

	update := d.statusUpdateLocked()
	d.adapter.bindStatusUpdate(d.view, &update)
	return &update
}

func (d *statusDelivery) statusUpdateLocked() ui.StatusUpdate {
	update := ui.StatusUpdate{Values: d.view.Values, Err: d.err, Watching: true}
	if received := d.received; received != 0 {
		update.Accept = func() { d.acceptStatus(received) }
	}
	return update
}

func (d *statusDelivery) readStatusUpdateLocked() ui.StatusUpdate {
	update := d.statusUpdateLocked()
	if d.err == nil {
		d.adapter.bindStatusUpdate(d.view, &update)
	}
	return update
}
