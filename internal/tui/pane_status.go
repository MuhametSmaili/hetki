package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/tui/list"
)

const (
	statusPollInterval  = 2 * time.Second
	statusReadTimeout   = 2 * time.Second
	statusSpinnerPeriod = 100 * time.Millisecond
	statusBlinkPeriod   = 750 * time.Millisecond
)

type StatusBindings map[list.ItemID][]status.PaneRef

type StatusOptions struct {
	Bindings StatusBindings
	Read     func(context.Context, status.Snapshot) StatusUpdate
}

// StatusUpdate replaces cached values and, when non-nil, their row bindings.
// Failed reads invalidate values while retaining the previous bindings.
type StatusUpdate struct {
	Values   status.Snapshot
	Bindings StatusBindings
	Err      error
}

func (b StatusBindings) StateAt(id list.ItemID, values status.Snapshot, now time.Time) status.State {
	state := status.Unreported
	for _, pane := range b[id] {
		candidate := values.At(pane, now)
		if statusPriority(candidate) > statusPriority(state) {
			state = candidate
		}
	}
	return state
}

func statusPriority(state status.State) int {
	switch state {
	case status.Blocked:
		return 4
	case status.Working:
		return 3
	case status.Unknown:
		return 2
	case status.Idle:
		return 1
	default:
		return 0
	}
}

type paneStatusState struct {
	options           StatusOptions
	values            status.Snapshot
	reading           bool
	frameDue          time.Time
	frameGeneration   uint64
	bindingGeneration uint64
	readErr           error
}

type statusPollMsg struct{}
type statusResultMsg struct {
	values     status.Snapshot
	bindings   StatusBindings
	generation uint64
	err        error
}
type statusFrameMsg struct{ generation uint64 }

func statusPoll(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return statusPollMsg{} })
}

func (m model) readPaneStatus() (tea.Model, tea.Cmd) {
	if m.paneStatus.options.Read == nil || m.quitting || m.navigation != "" || m.paneStatus.reading {
		return m, nil
	}
	if m.busy {
		return m, statusPoll(statusPollInterval)
	}
	m.paneStatus.reading = true
	read, previous, parent := m.paneStatus.options.Read, m.paneStatus.values, m.ctx
	generation := m.paneStatus.bindingGeneration
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, statusReadTimeout)
		defer cancel()
		update := read(ctx, previous)
		return statusResultMsg{values: update.Values, bindings: update.Bindings, generation: generation, err: update.Err}
	}
}

func (m model) handleStatusResult(msg statusResultMsg) (tea.Model, tea.Cmd) {
	m.paneStatus.reading = false
	if m.quitting || m.navigation != "" {
		return m, nil
	}
	if m.busy || msg.generation != m.paneStatus.bindingGeneration {
		return m, statusPoll(0)
	}
	m.paneStatus.values = msg.values
	m.paneStatus.readErr = msg.err
	if msg.bindings != nil {
		m.paneStatus.options.Bindings = msg.bindings
	}
	m.applyScope(m.items.Query(), time.Now())
	return m, statusPoll(statusPollInterval)
}

func (m model) syncStatusFrame(now time.Time) (model, tea.Cmd) {
	var due time.Time
	if !m.quitting && m.navigation == "" {
		due = m.nextStatusFrame(now)
	}
	p := &m.paneStatus
	if due.IsZero() {
		if !p.frameDue.IsZero() {
			p.frameGeneration++
			p.frameDue = time.Time{}
		}
		return m, nil
	}
	if !p.frameDue.IsZero() && !due.Before(p.frameDue) {
		return m, nil
	}
	p.frameDue = due
	p.frameGeneration++
	generation := p.frameGeneration
	return m, tea.Tick(due.Sub(now), func(time.Time) tea.Msg { return statusFrameMsg{generation: generation} })
}

func (m model) nextStatusFrame(now time.Time) time.Time {
	var due time.Time
	p := m.paneStatus
	for _, row := range m.items.VisibleRows() {
		switch p.options.Bindings.StateAt(row.Item.ID, p.values, now) {
		case status.Working:
			due = earlierStatusTime(due, now.Truncate(statusSpinnerPeriod).Add(statusSpinnerPeriod))
		case status.Blocked:
			due = earlierStatusTime(due, now.Truncate(statusBlinkPeriod).Add(statusBlinkPeriod))
		}
		for _, pane := range p.options.Bindings[row.Item.ID] {
			value := p.values.Lookup(pane)
			if value.ValidUntil.After(now) && value.At(now) != status.Unknown && value.State != status.Withdrawn {
				due = earlierStatusTime(due, value.ValidUntil)
			}
		}
	}
	return due
}

func earlierStatusTime(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}
