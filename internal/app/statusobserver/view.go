package statusobserver

import (
	"context"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/status/service"
)

// Changes returns a channel that closes at the next evidence, topology or
// validation change.
func (s *Source) Changes() <-chan struct{} {
	_, changes := s.core.Changes()
	return changes
}

// ReadView joins the cached topology and status under the inventory routing lock.
// It performs no native acquisition and never exposes references from another
// inventory generation. The caller owns the returned locations.
func (s *Source) ReadView(ctx context.Context) (daemon.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readView(ctx)
}

func (s *Source) readView(ctx context.Context) (daemon.View, error) {
	if err := ctx.Err(); err != nil {
		return daemon.View{}, err
	}
	if s.closed {
		return daemon.View{}, service.ErrClosed
	}
	if s.failure != nil {
		return daemon.View{}, s.failure
	}
	if !time.Now().Before(s.validatedUntil) {
		return daemon.View{}, daemon.ErrUnavailable
	}
	values, lease, err := s.core.ReadEvidence(ctx)
	if err != nil {
		return daemon.View{}, err
	}
	reported := make(map[string]bool, values.Len())
	for ref := range values.All() {
		reported[ref.ID] = true
	}
	view := daemon.View{Values: values, Lease: lease}
	for _, location := range s.locations {
		if reported[location.Pane] {
			view.Locations = append(view.Locations, daemon.Location{
				Session: location.Session, Window: location.Window, Pane: location.Pane,
			})
		}
	}
	return view, nil
}
