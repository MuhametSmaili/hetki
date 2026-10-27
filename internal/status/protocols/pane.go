// Package protocols coordinates terminal protocols for one pane. It owns framing,
// dispatch and protocol lifetimes without depending on acquisition or cache services.
package protocols

import (
	"bytes"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/protocols/internal/osc7501"
	"github.com/MuhametSmaili/hetki/internal/terminalstream"
)

// Options selects application evidence.
type Options struct {
	ProgramStatus bool
}

// Pane owns the state of all supported protocols in one pane lifetime. Construct
// it with New, serialize calls and do not copy it after use. It owns no workers.
type Pane struct {
	options      Options
	decoder      terminalstream.Decoder
	program      osc7501.Observer
	withdrawn    bool
	outcomeSince time.Time
}

// New creates an independent pane observer with immutable source selection.
func New(options Options) *Pane { return &Pane{options: options} }

// Feed consumes borrowed terminal bytes in order. The boolean says whether an
// returned observation should be published, including repeated reports and
// explicit withdrawal. When false, the observation is zero.
func (p *Pane) Feed(data []byte) (Observation, bool) {
	publish := false
	p.decoder.Feed(data, func(frame terminalstream.Frame) {
		_, previous := p.application()
		if p.handleFrame(frame) {
			p.observeOutcome(previous)
			publish = true
		}
	})
	if !publish {
		return Observation{}, false
	}
	return p.Snapshot(), true
}

func (p *Pane) observeOutcome(previous status.Outcome) {
	_, outcome := p.application()
	if outcome == previous {
		return
	}

	p.outcomeSince = time.Time{}
	if outcome != status.NoOutcome {
		p.outcomeSince = time.Now()
	}
}

// Reset discards partial input and all evidence after loss of continuity.
func (p *Pane) Reset() {
	p.decoder.Reset()
	p.program.Reset()
	p.withdrawn = false
	p.outcomeSince = time.Time{}
}

// ForegroundChanged ends application activity attribution while retaining explicit
// outcomes. The caller must establish continuous pane/root transport.
func (p *Pane) ForegroundChanged() {
	p.decoder.Reset()
	p.program.EndActivity()
}

func (p *Pane) handleFrame(frame terminalstream.Frame) bool {
	if frame.Kind == terminalstream.RIS {
		observed := p.program.Observed()
		p.program.Reset()
		p.withdrawn = p.withdrawn || observed
		return observed
	}

	if frame.Kind != terminalstream.OSC {
		return false
	}

	selector, payload, found := bytes.Cut(frame.Body, []byte(";"))
	if !found {
		return false
	}

	switch string(selector) {
	case "7501":
		if p.options.ProgramStatus && p.program.Handle(payload) == osc7501.Applied {
			return true
		}
		return false
	default:
		return false
	}
}

// Snapshot returns copied observations without exposing adapter records or buffers.
func (p *Pane) Snapshot() Observation {
	activity, outcome := p.application()
	return Observation{
		Activity:     activity,
		Outcome:      outcome,
		OutcomeSince: p.outcomeSince,
	}
}

func (p *Pane) application() (status.State, status.Outcome) {
	state, found := p.program.Root()
	if !found {
		if p.program.Observed() || p.withdrawn {
			return status.Withdrawn, status.NoOutcome
		}
		return status.Unreported, status.NoOutcome
	}

	switch state {
	case osc7501.Idle:
		return status.Idle, status.NoOutcome
	case osc7501.Working:
		return status.Working, status.NoOutcome
	case osc7501.Blocked:
		return status.Blocked, status.NoOutcome
	case osc7501.Done:
		return status.Unknown, status.Completed
	case osc7501.Error:
		return status.Unknown, status.Failed
	default:
		return status.Unknown, status.NoOutcome
	}
}
