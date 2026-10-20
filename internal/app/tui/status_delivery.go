package tui

import (
	"context"
	"fmt"
	"sync"

	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

// statusDelivery binds daemon views to the current picker rows. The adapter
// owns the bindings.
type statusDelivery struct {
	ctx      context.Context
	adapter  *LiveAdapter
	readView func(ctx context.Context, after uint64) (daemon.View, uint64, error)
	stop     func()

	mu       sync.Mutex
	view     daemon.View
	sequence uint64
	err      error
}

func (d *statusDelivery) options() ui.StatusOptions {
	return ui.StatusOptions{Read: d.readStatus}
}

// clear stops the reader and releases retained evidence. The owner cancels ctx
// and joins UI callbacks first.
func (d *statusDelivery) clear() {
	if d.stop != nil {
		d.stop()
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.view, d.sequence, d.err = daemon.View{}, 0, nil
}

// readStatus waits for a view newer than after. A failed read keeps the last
// locations but makes every value unknown. A dropped read stays quiet and
// keeps the old sequence.
func (d *statusDelivery) readStatus(ctx context.Context, after uint64) ui.StatusUpdate {
	var view daemon.View
	var sequence uint64
	var err error
	if ctx.Err() == nil {
		view, sequence, err = d.readView(ctx, after)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ctx.Err() != nil || ctx.Err() != nil {
		return ui.StatusUpdate{Values: d.view.Values.Invalidate(), Sequence: after}
	}

	d.sequence, d.err = sequence, nil
	if err != nil {
		d.view.Values = d.view.Values.Invalidate()
		d.err = fmt.Errorf("status unavailable: %w", err)
		return d.statusUpdateLocked()
	}

	d.view = view
	update := d.statusUpdateLocked()
	d.adapter.bindStatusUpdate(d.view, &update)
	return update
}

// projectionStatus binds the latest view to a new projection.
func (d *statusDelivery) projectionStatus() *ui.StatusUpdate {
	d.mu.Lock()
	defer d.mu.Unlock()

	update := d.statusUpdateLocked()
	d.adapter.bindStatusUpdate(d.view, &update)
	return &update
}

func (d *statusDelivery) statusUpdateLocked() ui.StatusUpdate {
	return ui.StatusUpdate{Values: d.view.Values, Err: d.err, Sequence: d.sequence}
}
