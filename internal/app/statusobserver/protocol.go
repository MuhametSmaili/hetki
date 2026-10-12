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
	pane              *protocols.Pane
	target            service.Target
	pendingWithdrawal bool // Carried only across a continuous foreground handoff until rebinding.
}

func (p *protocolBinding) reset() {
	p.pendingWithdrawal = false
	p.target = service.Target{}
	p.pane.Reset()
}

func (p *protocolBinding) processExited() {
	p.pendingWithdrawal = false
	p.target = service.Target{}
	p.pane.ProcessExited()
}

func (p *protocolBinding) foregroundChanged() {
	p.target = service.Target{}
	p.pane.ForegroundChanged()
	p.pendingWithdrawal = p.pane.Snapshot().Activity == status.Withdrawn
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

func (s *Source) bindProtocols() error {
	for _, row := range s.panes {
		if row.pane.Scope.Connection == "" {
			continue
		}

		if _, err := s.bindProtocol(row); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) bindProtocol(row sourcePane) (bool, error) {
	p := row.protocol
	if p == nil {
		return false, nil
	}

	target, available := s.core.Target(row.pane.Ref, row.pane.EvidenceSource())
	if !available {
		if row.facts.Dead {
			p.processExited()
		} else {
			p.reset()
		}
		return false, nil
	}

	if p.target != (service.Target{}) && !p.target.SameGeneration(target) {
		p.reset()
	}
	p.target = target
	if !p.pendingWithdrawal {
		return true, nil
	}

	p.pendingWithdrawal = false
	return s.applyProtocol(p, status.Withdrawn)
}

func (s *Source) protocolOutput(row sourcePane, data []byte) error {
	available, err := s.bindProtocol(row)
	if err != nil || !available {
		return err
	}

	p := row.protocol
	observation, publish := p.pane.Feed(data)
	if !publish {
		return nil
	}

	_, err = s.applyProtocol(p, observation.Activity)
	return err
}

func (s *Source) applyProtocol(p *protocolBinding, activity status.State) (bool, error) {
	s.protocolSequence++
	err := s.core.Apply(p.target, s.protocolSequence, status.Value{State: activity})
	if errors.Is(err, service.ErrStale) {
		p.reset()
		return false, nil
	}
	return err == nil, err
}
