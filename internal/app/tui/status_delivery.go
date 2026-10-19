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

// statusDelivery owns one picker's received evidence and UI acknowledgements.
// The adapter owns bindings. Only the retained delivery supplies the next cursor.
type statusDelivery struct {
	ctx      context.Context
	adapter  *LiveAdapter
	readView func(context.Context, daemon.Cursor) (daemon.View, error)

	mu       sync.Mutex
	view     daemon.View
	err      error
	received uint64
	applied  uint64
}

func (d *statusDelivery) options() ui.StatusOptions {
	return ui.StatusOptions{Read: d.readStatus, ReadTimeout: daemon.WatchTimeout + time.Second}
}

// clear releases retained evidence after the owner cancels and joins callbacks.
func (d *statusDelivery) clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.view, d.err = daemon.View{}, nil
	d.received, d.applied = 0, 0
}

func (d *statusDelivery) readStatus(ctx context.Context, previous status.Snapshot) ui.StatusUpdate {
	if update, ok := d.replayStatus(ctx); ok {
		return update
	}
	if err := ctx.Err(); err != nil {
		return d.receiveStatus(ctx, daemon.View{}, previous, err)
	}

	d.mu.Lock()
	after := d.view.Cursor
	d.mu.Unlock()

	view, err := d.readView(ctx, after)
	return d.receiveStatus(ctx, view, previous, err)
}

func (d *statusDelivery) receiveStatus(ctx context.Context, view daemon.View, previous status.Snapshot, err error) ui.StatusUpdate {
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
		view.Cursor = daemon.Cursor{}
	}
	d.view = view
	d.err = nil
	if err != nil && !errors.Is(contextErr, context.Canceled) {
		d.err = fmt.Errorf("status unavailable: %w", err)
	}
	d.received++
	return d.readStatusUpdateLocked()
}

// A watch cursor records receipt, not UI acceptance. Rebind an unaccepted
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
	update := ui.StatusUpdate{
		Values: d.view.Values, Err: d.err,
		Watching: d.view.Cursor.Revision != 0,
	}
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
