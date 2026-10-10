package statusservice

import (
	"fmt"
	"strings"
	"time"

	"github.com/MSmaili/hetki/internal/status"
)

// Target is issued before collection. Generation checks prevent a result from
// crossing an observed lifetime/configuration change, including change-and-back.
type Target struct {
	Pane       Pane
	Source     Source
	generation uint64
	service    *Service
}

func (s *Service) Target(pane status.PaneRef, source Source) (Target, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || !time.Now().Before(s.validUntil) {
		return Target{}, false
	}

	row, exists := s.panes[pane.ID]
	if !exists || row.pane.Ref != pane || row.pane.source() != source || !row.pane.available() {
		return Target{}, false
	}

	return Target{
		Pane:       row.pane,
		Source:     row.pane.source(),
		generation: row.generation,
		service:    s,
	}, true
}

// Apply accepts only the selected source. Sequence increases in pane/source order
// and is assigned before acquisition, not when asynchronous work finishes.
// Protocol values have no lease; screen activity carries a capture deadline.
func (s *Service) Apply(target Target, sequence uint64, value status.Value) error {
	if err := validateEvidence(target.Source, sequence, value); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}

	now := time.Now()
	row, exists := s.panes[target.Pane.Ref.ID]
	if !exists || target.service != s || !row.accepts(target, sequence, s.validUntil, now) {
		return ErrStale
	}

	if !value.ValidUntil.IsZero() && !now.Before(value.ValidUntil) {
		return ErrStale
	}

	if row.value != value {
		value.State = status.State(strings.Clone(string(value.State)))
		s.changed()
		row.value = value
	}
	row.sequence = sequence
	s.panes[row.pane.Ref.ID] = row
	return nil
}

func (r record) accepts(target Target, sequence uint64, validUntil, now time.Time) bool {
	return sameEvidenceScope(r.pane, target.Pane) && r.generation == target.generation &&
		r.pane.source() == target.Source && r.pane.available() &&
		sequence > r.sequence && now.Before(validUntil)
}

func validateEvidence(source Source, sequence uint64, value status.Value) error {
	if sequence == 0 {
		return fmt.Errorf("status evidence requires a sequence")
	}

	switch value.State {
	case status.Unknown, status.Withdrawn:
		if !value.ValidUntil.IsZero() {
			return fmt.Errorf("inactive status evidence cannot have a deadline")
		}
	case status.Idle, status.Working, status.Blocked:
	default:
		return fmt.Errorf("invalid status evidence state")
	}

	switch source {
	case OSC7501:
		if !value.ValidUntil.IsZero() {
			return fmt.Errorf("protocol evidence cannot have a heartbeat lease")
		}
	case Screen:
		if value.State == status.Withdrawn || value.State != status.Unknown && value.ValidUntil.IsZero() {
			return fmt.Errorf("screen evidence requires activity with a deadline or unknown")
		}
	default:
		return fmt.Errorf("unsupported status evidence source")
	}

	return nil
}
