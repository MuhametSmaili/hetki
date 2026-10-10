// Package protocols coordinates terminal protocols for one pane. It owns framing,
// dispatch and protocol lifetimes without depending on acquisition or cache services.
package protocols

import (
	"bytes"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/protocols/internal/osc133"
	"github.com/MSmaili/hetki/internal/status/protocols/internal/osc7501"
	"github.com/MSmaili/hetki/internal/terminalstream"
)

// Options selects application evidence independently of shell lifecycle tracking.
type Options struct {
	ProgramStatus bool
}

// Pane owns the state of all supported protocols in one pane lifetime. Construct
// it with New, serialize calls and do not copy it after use. It owns no workers.
type Pane struct {
	options   Options
	decoder   terminalstream.Decoder
	program   osc7501.Observer
	shell     osc133.Observer
	withdrawn bool
}

// New creates an independent pane observer with immutable source selection.
func New(options Options) *Pane { return &Pane{options: options} }

// Feed consumes borrowed terminal bytes in order. The boolean says whether an
// returned observation should be published, including repeated reports and
// explicit withdrawal. When false, the observation is zero; shell-only changes
// are available through Snapshot without projecting activity on every byte chunk.
func (p *Pane) Feed(data []byte) (Observation, bool) {
	publish := false
	p.decoder.Feed(data, func(frame terminalstream.Frame) {
		if p.handleFrame(frame) {
			publish = true
		}
	})
	if !publish {
		return Observation{}, false
	}
	return p.Snapshot(), true
}

// Reset discards partial input and all evidence after loss of continuity.
func (p *Pane) Reset() {
	p.decoder.Reset()
	p.program.Reset()
	p.shell.Reset()
	p.withdrawn = false
}

// ForegroundChanged ends application activity attribution while preserving shell
// command matching. The caller must establish continuous pane/root transport.
func (p *Pane) ForegroundChanged() {
	p.decoder.Reset()
	p.program.EndActivity()
}

// ProcessExited ends the attached process lifetime, retaining explicit outcomes
// until this pane is reset or retired. Shell matching ends with the process.
func (p *Pane) ProcessExited() {
	p.ForegroundChanged()
	p.shell.Reset()
}

func (p *Pane) handleFrame(frame terminalstream.Frame) bool {
	if frame.Kind == terminalstream.RIS {
		observed := p.program.Observed()
		p.program.Reset()
		p.shell.Reset()
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
		return p.options.ProgramStatus && p.program.Handle(payload) == osc7501.Applied
	case "133":
		return p.shellBoundary(payload)
	default:
		return false
	}
}

func (p *Pane) shellBoundary(payload []byte) bool {
	if p.shell.Handle(payload) != osc133.Prompt {
		return false
	}

	p.program.EndActivity()
	return p.program.Observed()
}

// Snapshot returns copied observations without exposing adapter records or buffers.
func (p *Pane) Snapshot() Observation {
	activity, outcome := p.application()
	return Observation{
		Activity: activity,
		Outcome:  outcome,
		Shell:    shellObservation(p.shell.Snapshot()),
	}
}

func (p *Pane) application() (status.State, Outcome) {
	state, found := p.program.Lookup("")
	if !found {
		if p.program.Observed() || p.withdrawn {
			return status.Withdrawn, NoOutcome
		}
		return status.Unreported, NoOutcome
	}

	switch state {
	case osc7501.Idle:
		return status.Idle, NoOutcome
	case osc7501.Working:
		return status.Working, NoOutcome
	case osc7501.Blocked:
		return status.Blocked, NoOutcome
	case osc7501.Done:
		return status.Unknown, Completed
	case osc7501.Error:
		return status.Unknown, Failed
	default:
		return status.Unknown, NoOutcome
	}
}
