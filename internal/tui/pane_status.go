package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

const (
	statusPollInterval  = 2 * time.Second
	statusSpinnerPeriod = 100 * time.Millisecond
)

type StatusBindings map[list.ItemID][]status.PaneRef

// StatusOptions connects the picker to a status source. Read waits for a result
// newer than after, or for the end of ctx.
type StatusOptions struct {
	Bindings StatusBindings
	Read     func(ctx context.Context, after uint64) StatusUpdate
}

// StatusUpdate replaces cached values and, when non-nil, their row bindings and
// presentation candidates. Failed reads invalidate values while retaining both.
type StatusUpdate struct {
	Values   status.Snapshot
	Bindings StatusBindings
	Err      error

	// Windows replaces tree-pane presentation candidates when non-nil.
	Windows map[list.ItemID][]WindowPane

	// Sequence identifies this result. After the UI applies it, the next read
	// waits for a newer result. A discarded result is read again.
	Sequence uint64
}

func (b StatusBindings) StateAt(id list.ItemID, values status.Snapshot, now time.Time) status.State {
	return b.ValueAt(id, values, now).State
}

// ValueAt returns the highest-priority bound activity and outcome, resolving freshness
// and the brief success indication before aggregation.
// Attention and ongoing work take precedence over completed results in other panes.
func (b StatusBindings) ValueAt(id list.ItemID, values status.Snapshot, now time.Time) status.Value {
	var value status.Value
	for _, pane := range b[id] {
		reported := values.Lookup(pane)
		candidate := displayedStatus(reported, now)
		if statusPriority(candidate) > statusPriority(value) {
			value = candidate
		}
	}
	return value
}

func statusPriority(value status.Value) int {
	switch value.State {
	case status.Blocked:
		return 6
	case status.Working:
		return 5
	case status.Unknown:
		switch value.Outcome {
		case status.Failed:
			return 4
		case status.Completed:
			return 2
		default:
			return 3
		}
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
	sequence          uint64
	windows           map[list.ItemID][]WindowPane
	representatives   map[list.ItemID]windowRepresentative
	windowChangeDue   time.Time
}

type statusPollMsg struct{}
type statusResultMsg struct {
	update     StatusUpdate
	generation uint64
}
type statusFrameMsg struct{ generation uint64 }

func statusPoll(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return statusPollMsg{} })
}

func (m model) readPaneStatus() (tea.Model, tea.Cmd) {
	if m.paneStatus.options.Read == nil || m.quitting || !m.navigation.IsZero() || m.paneStatus.reading {
		return m, nil
	}
	if m.busy {
		return m, statusPoll(statusPollInterval)
	}
	m.paneStatus.reading = true
	read, after, ctx := m.paneStatus.options.Read, m.paneStatus.sequence, m.ctx
	generation := m.paneStatus.bindingGeneration

	return m, func() tea.Msg {
		return statusResultMsg{update: read(ctx, after), generation: generation}
	}
}

func (m model) handleStatusResult(msg statusResultMsg) (tea.Model, tea.Cmd) {
	m.paneStatus.reading = false
	if m.quitting || !m.navigation.IsZero() {
		return m, nil
	}
	if m.busy || msg.generation != m.paneStatus.bindingGeneration {
		return m, statusPoll(0)
	}
	m.applyStatusUpdate(msg.update)
	m.applyStatusPresentation(time.Now())

	// Read waits for a newer result, so the next read starts at once even after
	// an error: the source applies its own retry delay.
	return m, statusPoll(0)
}

func (m *model) applyStatusUpdate(update StatusUpdate) {
	m.paneStatus.values = update.Values
	m.paneStatus.readErr = update.Err
	if update.Bindings != nil {
		m.paneStatus.options.Bindings = update.Bindings
	}
	if update.Windows != nil {
		m.paneStatus.windows = update.Windows
	}
	m.paneStatus.sequence = update.Sequence
}

func (m model) syncStatusFrame(now time.Time) (model, tea.Cmd) {
	var due time.Time
	if !m.quitting && m.navigation.IsZero() {
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
	p := m.paneStatus
	// Hidden window details still affect search when their status expires.
	due := p.windowChangeDue
	for _, row := range m.items.VisibleRows() {
		switch p.rowValue(row.Item.ID, now).State {
		case status.Working:
			due = earlierStatusTime(due, now.Truncate(statusSpinnerPeriod).Add(statusSpinnerPeriod))
		}
		if _, ok := p.windows[row.Item.ID]; ok {
			continue
		}
		for _, pane := range p.options.Bindings[row.Item.ID] {
			value := p.values.Lookup(pane)
			if change := nextStatusChange(value, now); !change.IsZero() {
				due = earlierStatusTime(due, change)
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
