package tui

import (
	"context"
	"sync"

	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type uiEffects struct {
	ctx    context.Context
	cancel context.CancelFunc
	driver Driver
	status *statusDelivery
	mu     sync.Mutex
	active sync.WaitGroup
}

func (e *uiEffects) begin() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ctx.Err(); err != nil {
		return err
	}
	e.active.Add(1)
	return nil
}

func (e *uiEffects) stop() {
	// Close admission before waiting, including callbacks not yet started by Tea.
	e.mu.Lock()
	e.cancel()
	e.mu.Unlock()
	e.active.Wait()

	if e.status != nil {
		e.status.clear()
	}
}

func (e *uiEffects) dispatch(request ui.ActionRequest) (ui.ActionResult, error) {
	if err := e.begin(); err != nil {
		return ui.ActionResult{}, err
	}
	defer e.active.Done()
	result, err := e.driver.Execute(e.ctx, request)
	if err == nil && result.Snapshot != nil && e.status != nil {
		result.Status = e.status.projectionStatus()
	}
	return result, err
}

func (e *uiEffects) preview(ctx context.Context, id list.ItemID) (string, error) {
	if err := e.begin(); err != nil {
		return "", err
	}
	defer e.active.Done()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	readCtx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return e.driver.Preview(readCtx, id)
}

func (e *uiEffects) statusOptions() ui.StatusOptions {
	if e.status == nil {
		return ui.StatusOptions{}
	}
	options := e.status.options()
	options.Read = e.readStatus
	return options
}

func (e *uiEffects) readStatus(ctx context.Context, after uint64) ui.StatusUpdate {
	if err := e.begin(); err != nil {
		return ui.StatusUpdate{Sequence: after}
	}
	defer e.active.Done()
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	return e.status.readStatus(readCtx, after)
}
