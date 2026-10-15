package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

type statusDelivery struct {
	view     daemon.View
	err      error
	received uint64
	applied  uint64
}

func (e *uiEffects) receiveStatus(ctx context.Context, view daemon.View, previous status.Snapshot, err error) ui.StatusUpdate {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ctx.Err() != nil {
		return ui.StatusUpdate{Values: previous.Invalidate()}
	}
	contextErr := ctx.Err()
	if contextErr != nil {
		err = contextErr
	}
	if err != nil {
		view = e.delivery.view
		if e.delivery.received == 0 {
			view.Values = previous
		}
		view.Values = view.Values.Invalidate()
		view.Cursor = daemon.Cursor{}
	}
	e.delivery.view = view
	e.delivery.err = nil
	if err != nil && !errors.Is(contextErr, context.Canceled) {
		e.delivery.err = fmt.Errorf("status unavailable: %w", err)
	}
	e.delivery.received++
	return e.readStatusUpdateLocked()
}

// A watch cursor records receipt, not UI acceptance. Rebind an unaccepted
// delivery instead of waiting for an unrelated daemon change.
func (e *uiEffects) replayStatus(ctx context.Context) (ui.StatusUpdate, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ctx.Err() != nil || ctx.Err() != nil || e.delivery.received == e.delivery.applied {
		return ui.StatusUpdate{}, false
	}
	return e.readStatusUpdateLocked(), true
}

func (e *uiEffects) acceptStatus(received uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ctx.Err() == nil {
		e.delivery.applied = max(e.delivery.applied, received)
	}
}

func (e *uiEffects) projectionStatus() *ui.StatusUpdate {
	if e.reader == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	update := e.statusUpdateLocked()
	if _, ok := e.reader.(statusViewReader); ok && e.delivery.received != 0 {
		update.Bindings = e.bindReceivedView()
	} else {
		update.Bindings = e.statusBindings()
	}
	return &update
}

func (e *uiEffects) statusUpdateLocked() ui.StatusUpdate {
	update := ui.StatusUpdate{
		Values: e.delivery.view.Values, Err: e.delivery.err,
		Watching: e.delivery.view.Cursor.Revision != 0,
	}
	if received := e.delivery.received; received != 0 {
		update.Accept = func() { e.acceptStatus(received) }
	}
	return update
}

func (e *uiEffects) readStatusUpdateLocked() ui.StatusUpdate {
	update := e.statusUpdateLocked()
	if _, ok := e.reader.(statusViewReader); ok && e.delivery.err == nil {
		update.Bindings = e.bindReceivedView()
	}
	return update
}

// The view's values and locations stay together; row bindings are always rebuilt
// against the driver's current projection and exact server incarnation.
func (e *uiEffects) bindReceivedView() ui.StatusBindings {
	if driver, ok := e.driver.(interface {
		bindStatusView(daemon.View) ui.StatusBindings
	}); ok {
		return driver.bindStatusView(e.delivery.view)
	}
	return make(ui.StatusBindings)
}
