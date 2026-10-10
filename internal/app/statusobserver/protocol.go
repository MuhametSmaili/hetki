package statusobserver

import (
	"bytes"
	"errors"

	"github.com/MSmaili/hetki/internal/status/adapters/osc7501"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/MSmaili/hetki/internal/terminalstream"
)

// Stored by pointer: decoder buffers and reducer records must not be copied with
// inventory rows. Source.mu serializes all access, including lifecycle changes.
type paneProtocol struct {
	decoder  terminalstream.Decoder
	observer osc7501.Observer
	target   service.Target
}

func (p *paneProtocol) resetInput(retainOutcomes bool) {
	p.decoder.Reset()
	p.target = service.Target{}
	if retainOutcomes {
		p.observer.EndActivity()
	} else {
		p.observer.Reset()
	}
}

func reconcileProtocol(previous, next sourcePane) *paneProtocol {
	p := previous.protocol
	if p == nil || previous.pane.Ref != next.pane.Ref || previous.pane.Profile != next.pane.Profile {
		return nil
	}

	if next.facts.Dead {
		p.resetInput(true)
	} else if !next.valid || previous.pane.Scope != next.pane.Scope {
		// A verified foreground handoff ends activity attribution, not the pane's
		// explicit outcome. Missing metadata and transport gaps prove no exit.
		retain := previous.valid && next.valid && previous.facts.Root == next.facts.Root &&
			next.facts.Foreground.Identity() != "" && previous.facts.Foreground.Identity() != ""
		p.resetInput(retain)
	}
	return p
}

func (s *Source) bindProtocol(row sourcePane) bool {
	p := row.protocol
	if p == nil {
		return false
	}

	target, available := s.core.Target(row.pane.Ref, service.OSC7501)
	if !available {
		p.resetInput(row.facts.Dead)
		return false
	}

	if p.target != (service.Target{}) && !p.target.SameGeneration(target) {
		p.resetInput(false)
	}
	p.target = target
	return true
}

func (s *Source) protocolOutput(row sourcePane, data []byte) error {
	if !s.bindProtocol(row) {
		return nil
	}

	p := row.protocol
	changed := false
	p.decoder.Feed(data, func(frame terminalstream.Frame) {
		switch frame.Kind {
		case terminalstream.RIS:
			changed = changed || p.observer.Observed()
			p.observer.Reset()
		case terminalstream.OSC:
			if payload, found := bytes.CutPrefix(frame.Body, []byte("7501;")); found {
				if p.observer.Handle(payload) == osc7501.Applied {
					changed = true
				}
			}
		}
	})
	if !changed {
		return nil
	}

	// A chunk may contain many reports. Publish only its final reduction; each
	// report still participates in ordering, subtree clears and record eviction.
	s.protocolSequence++
	err := s.core.Apply(p.target, s.protocolSequence, osc7501Value(&p.observer))
	if errors.Is(err, service.ErrStale) {
		p.resetInput(false)
		return nil
	}
	return err
}
