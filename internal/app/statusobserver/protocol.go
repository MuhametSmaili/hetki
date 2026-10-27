package statusobserver

import (
	"errors"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/protocols"
	"github.com/MuhametSmaili/hetki/internal/status/service"
)

// protocolBinding joins an encapsulated pane observer to the cache target. All
// access is serialized by Source.mu; native identities stay in this package.
type protocolBinding struct {
	pane               *protocols.Pane
	target             service.Target
	pendingPublication bool // A continuous launch or withdrawal awaiting validated rebinding.
}

func (p *protocolBinding) reset() {
	p.pendingPublication = false
	p.target = service.Target{}
	p.pane.Reset()
}

func (p *protocolBinding) processExited() {
	p.pendingPublication = false
	p.target = service.Target{}
	p.pane.ForegroundChanged()
}

func (p *protocolBinding) foregroundChanged(startingChild bool) {
	p.target = service.Target{}
	// Output can report the child's activity before inventory discovers its group.
	// Entering that group refines attribution on the same uninterrupted pane stream.
	if !startingChild {
		p.pane.ForegroundChanged()
	}
	observation := p.pane.Snapshot()
	p.pendingPublication = observation.Activity == status.Withdrawn || observation.Outcome != status.NoOutcome || startingChild && observation.Activity != status.Unreported
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
		p.foregroundChanged(previous.Foreground == previous.Root)
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
			p.foregroundChanged(previous.facts.Foreground == previous.facts.Root && next.facts.Foreground != next.facts.Root)
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
	if !p.pendingPublication {
		return true, nil
	}

	p.pendingPublication = false
	return s.applyProtocol(p, p.pane.Snapshot())
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

	_, err = s.applyProtocol(p, observation)
	return err
}

func (s *Source) applyProtocol(p *protocolBinding, observation protocols.Observation) (bool, error) {
	s.protocolSequence++
	err := s.core.Apply(p.target, s.protocolSequence, status.Value{
		State:        observation.Activity,
		Outcome:      observation.Outcome,
		OutcomeSince: observation.OutcomeSince,
		OutcomeEnded: observation.OutcomeEnded,
	})
	if errors.Is(err, service.ErrStale) {
		p.reset()
		return false, nil
	}
	return err == nil, err
}
