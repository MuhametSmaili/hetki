package statusobserver

import (
	"errors"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/protocols"
	"github.com/MSmaili/hetki/internal/status/service"
)

// protocolBinding joins an encapsulated pane observer to the cache target. All
// access is serialized by Source.mu; native identities stay in this package.
type protocolBinding struct {
	pane   *protocols.Pane
	target service.Target
}

func (p *protocolBinding) reset() {
	p.target = service.Target{}
	p.pane.Reset()
}

func (p *protocolBinding) processExited() {
	p.target = service.Target{}
	p.pane.ProcessExited()
}

func (p *protocolBinding) foregroundChanged() {
	p.target = service.Target{}
	p.pane.ForegroundChanged()
}

func (s *Source) expireProtocols(now time.Time) {
	if now.Before(s.validatedUntil) {
		return
	}

	for _, row := range s.panes {
		if row.protocol != nil {
			row.protocol.reset()
		}
	}
}

func (p *protocolBinding) resetStream(previous, next Scope, dead bool) {
	if dead {
		p.processExited()
		return
	}

	if next.Connection != "" && next.Connection == previous.Connection && next.Root == previous.Root &&
		next.Foreground != previous.Foreground {
		p.foregroundChanged()
		return
	}
	p.reset()
}

func reconcileProtocol(previous, next sourcePane) *protocolBinding {
	p := previous.protocol
	if p == nil || previous.pane.Ref != next.pane.Ref || previous.pane.Profile != next.pane.Profile {
		return nil
	}

	if next.facts.Dead {
		p.processExited()
	} else if !next.valid || previous.pane.Scope != next.pane.Scope {
		continuous := previous.valid && next.valid && previous.facts.Root == next.facts.Root &&
			next.facts.Foreground.Identity() != "" && previous.facts.Foreground.Identity() != ""
		if continuous {
			p.foregroundChanged()
		} else {
			p.reset()
		}
	}
	return p
}

func (s *Source) bindProtocol(row sourcePane) bool {
	p := row.protocol
	if p == nil {
		return false
	}

	target, available := s.core.Target(row.pane.Ref, row.pane.EvidenceSource())
	if !available {
		if row.facts.Dead {
			p.processExited()
		} else {
			p.reset()
		}
		return false
	}

	if p.target != (service.Target{}) && !p.target.SameGeneration(target) {
		p.reset()
	}
	p.target = target
	return true
}

func (s *Source) protocolOutput(row sourcePane, data []byte) error {
	if !s.bindProtocol(row) {
		return nil
	}

	p := row.protocol
	observation, publish := p.pane.Feed(data)
	if !publish {
		return nil
	}

	s.protocolSequence++
	err := s.core.Apply(p.target, s.protocolSequence, status.Value{State: observation.Activity})
	if errors.Is(err, service.ErrStale) {
		p.reset()
		return nil
	}
	return err
}
