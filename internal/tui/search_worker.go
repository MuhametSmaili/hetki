package tui

import (
	"context"
	"sync"

	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type searchResultMsg struct {
	key         list.SearchKey
	result      list.SearchResult
	err         error
	replacement *replacementRequest
	prepared    *model
}

type pickerWork struct {
	search      *list.SearchRequest
	replacement *replacementRequest
}

// Run owns this worker and joins it on every exit. Only the worker evaluates
// requests; Submit replaces the single pending slot and cancels running work.
type searchWorker struct {
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	wake     chan struct{}
	results  chan searchResultMsg
	evaluate func(context.Context, list.SearchRequest) (list.SearchResult, error)
	searcher list.Searcher

	mu      sync.Mutex
	pending *pickerWork
	running context.CancelFunc
}

func newSearchWorker(ctx context.Context, evaluate func(context.Context, list.SearchRequest) (list.SearchResult, error)) *searchWorker {
	ctx, cancel := context.WithCancel(ctx)
	w := &searchWorker{
		ctx: ctx, cancel: cancel, done: make(chan struct{}),
		wake: make(chan struct{}, 1), results: make(chan searchResultMsg, 1),
		evaluate: evaluate,
	}
	if w.evaluate == nil {
		w.evaluate = w.searcher.Evaluate
	}
	go w.run()
	return w
}

func (w *searchWorker) submit(request *list.SearchRequest) {
	var work *pickerWork
	if request != nil {
		queued := *request
		work = &pickerWork{search: &queued}
	}
	w.submitWork(work)
}

func (w *searchWorker) submitWork(work *pickerWork) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running != nil {
		w.running()
	}
	w.pending = nil
	if w.ctx.Err() == nil {
		w.pending = work
	}
	select {
	case <-w.results:
	default:
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *searchWorker) close() {
	w.cancel()
	<-w.done
}

func (w *searchWorker) run() {
	defer close(w.done)
	defer close(w.results)
	defer w.release()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.wake:
			w.evaluatePending()
		}
	}
}

func (w *searchWorker) evaluatePending() {
	w.mu.Lock()
	request := w.pending
	w.pending = nil
	if request == nil || w.ctx.Err() != nil {
		w.searcher.Reset()
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	w.running = cancel
	w.mu.Unlock()

	result := w.evaluateWork(ctx, *request)
	w.mu.Lock()
	defer w.mu.Unlock()
	defer cancel()
	w.running = nil
	if ctx.Err() != nil {
		return
	}
	select {
	case <-w.results:
	default:
	}
	w.results <- result
}

func (w *searchWorker) evaluateWork(ctx context.Context, work pickerWork) searchResultMsg {
	if work.replacement != nil {
		w.searcher.Reset()
		prepared, err := work.replacement.evaluate(ctx)
		return searchResultMsg{replacement: work.replacement, prepared: prepared, err: err}
	}
	result, err := w.evaluate(ctx, *work.search)
	return searchResultMsg{key: work.search.Key(), result: result, err: err}
}

func (w *searchWorker) release() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending, w.running = nil, nil
	w.searcher.Reset()
	select {
	case <-w.results:
	default:
	}
}
