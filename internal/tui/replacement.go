package tui

import (
	"context"
	"maps"
	"time"

	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// A request is immutable after submission. Its identity rejects superseded
// deliveries independently of cancellation. The shared worker owns evaluation.
type replacementRequest struct {
	base    model
	message actionResultMsg
}

func (m model) needsReplacement(msg actionResultMsg) bool {
	r := msg.result
	if r.Snapshot == nil {
		return false
	}
	if r.Choice != nil {
		return !r.Choice.Closed || (m.choice != nil && m.choice.parent != nil)
	}
	return msg.err == nil && r.Menu == nil && r.Input == nil && r.Confirmation == nil && r.Navigation.IsZero()
}

func (m model) queueReplacement(msg actionResultMsg) model {
	base := m
	base.replacement = nil
	base.search = searchState{}
	base.preview.cancel = nil
	if m.choice != nil {
		choice := *m.choice
		choice.history = maps.Clone(choice.history)
		if choice.parent != nil {
			parent := *choice.parent
			choice.parent = &parent
		}
		base.choice = &choice
	}
	request := &replacementRequest{base: base, message: msg}
	m.replacement = request
	m.busy, m.status = true, "preparing items…"
	m.search.selection, m.search.confirm = nil, nil
	m.search.active = false
	m.search.worker.submitWork(&pickerWork{replacement: request})
	return m
}

func (r *replacementRequest) evaluate(ctx context.Context) (*model, error) {
	candidate := r.base
	candidate.ctx = ctx
	if candidate.choice != nil {
		choice := *candidate.choice
		candidate.choice = &choice
	}
	source := candidate.items
	closing := r.message.result.Choice != nil && r.message.result.Choice.Closed
	if closing {
		source = candidate.choice.parent.items
	}
	items, err := source.Prepare(ctx, func(items *list.Model) error {
		candidate.items = *items
		if r.message.result.Choice != nil && !closing {
			// Choice parents retain the live list, never the temporary preparation
			// context or synchronous filtering mode.
			candidate.items = r.base.items
		}
		if closing {
			parent := *candidate.choice.parent
			parent.items = *items
			choice := *candidate.choice
			choice.parent = &parent
			candidate.choice = &choice
		}
		next, _ := candidate.handleActionResult(r.message)
		candidate = next.(model) //nolint:forcetypeassert // action handlers return the concrete model.
		*items = candidate.items
		return nil
	})
	if err != nil {
		return nil, err
	}
	candidate.items = items
	candidate.items.DeferSearch()
	return &candidate, nil
}

func (m model) acceptReplacement(msg searchResultMsg) model {
	if m.quitting || m.replacement != msg.replacement {
		return m
	}
	if msg.err != nil {
		m.replacement = nil
		m.busy, m.status, m.err = false, "", msg.err
		m.pending, m.pendingRows = nil, nil
		return m
	}
	prepared := msg.prepared
	if due := prepared.nextPresentationChange(); prepared.err == nil && !due.IsZero() && !time.Now().Before(due) {
		return m.queueReplacement(msg.replacement.message)
	}
	m.replacement = nil
	m.items, m.choice, m.scope = prepared.items, prepared.choice, prepared.scope
	m.view, m.mode, m.input = prepared.view, prepared.mode, prepared.input
	m.busy, m.status, m.err = prepared.busy, prepared.status, prepared.err
	m.pending, m.pendingRows = prepared.pending, prepared.pendingRows
	m.search.selection = prepared.search.selection
	// Poll/read and timer ownership remain with the live UI, including replies
	// received while preparation was running. Only prepared data is replaced.
	status := prepared.paneStatus
	status.reading = m.paneStatus.reading
	status.frameDue, status.frameGeneration = m.paneStatus.frameDue, m.paneStatus.frameGeneration
	m.paneStatus = status
	m.preview.enabled = prepared.preview.enabled
	m.preview.invalidate()
	return m.reflow()
}
