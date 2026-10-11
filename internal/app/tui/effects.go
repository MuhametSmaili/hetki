package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/daemon"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
)

type uiEffects struct {
	ctx    context.Context
	cancel context.CancelFunc
	driver Driver
	reader StatusReader
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
}

func (e *uiEffects) dispatch(request ui.ActionRequest) (ui.ActionResult, error) {
	if err := e.begin(); err != nil {
		return ui.ActionResult{}, err
	}
	defer e.active.Done()
	result, err := e.driver.Execute(e.ctx, request)
	if err == nil && result.Snapshot != nil {
		result.StatusBindings = e.statusBindings()
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
	if e.reader == nil {
		return ui.StatusOptions{}
	}
	return ui.StatusOptions{Bindings: e.statusBindings(), Read: e.readStatus}
}

func (e *uiEffects) statusBindings() ui.StatusBindings {
	if e.reader == nil {
		return nil
	}
	if driver, ok := e.driver.(interface{ StatusBindings() ui.StatusBindings }); ok {
		return driver.StatusBindings()
	}
	return nil
}

func (e *uiEffects) readStatus(ctx context.Context, previous status.Snapshot) ui.StatusUpdate {
	if err := e.begin(); err != nil {
		return ui.StatusUpdate{Values: previous.Invalidate()}
	}
	defer e.active.Done()
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	if reader, ok := e.reader.(statusViewReader); ok {
		return e.readView(readCtx, reader, previous)
	}
	values, err := ReadStatus(readCtx, e.reader, previous)
	return ui.StatusUpdate{Values: values, Err: statusReadError(readCtx, err)}
}

type statusViewReader interface {
	ReadView(context.Context) (daemon.View, error)
}

func (e *uiEffects) readView(ctx context.Context, reader statusViewReader, previous status.Snapshot) ui.StatusUpdate {
	view, err := reader.ReadView(ctx)
	if err != nil || ctx.Err() != nil {
		return ui.StatusUpdate{Values: previous.Invalidate(), Err: statusReadError(ctx, err)}
	}
	update := ui.StatusUpdate{Values: view.Values, Bindings: make(ui.StatusBindings)}
	if driver, ok := e.driver.(interface {
		bindStatusView(daemon.View) ui.StatusBindings
	}); ok {
		update.Bindings = driver.bindStatusView(view)
	}
	return update
}

func statusReadError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		return nil
	}
	return fmt.Errorf("status unavailable: %w", err)
}
