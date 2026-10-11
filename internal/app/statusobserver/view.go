package statusobserver

import (
	"context"
	"time"

	"github.com/MSmaili/hetki/internal/status/daemon"
	"github.com/MSmaili/hetki/internal/status/service"
)

// ReadView joins the cached topology and status under the inventory routing lock.
// It performs no native acquisition and never exposes references from another
// inventory generation. The caller owns the returned locations.
func (s *Source) ReadView(ctx context.Context) (daemon.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	values, err := s.core.ReadStatus(ctx)
	if err != nil {
		return daemon.View{}, err
	}
	reported := make(map[string]bool, values.Len())
	for ref := range values.All() {
		reported[ref.ID] = true
	}
	view := daemon.View{Values: values}
	for _, location := range s.locations {
		if reported[location.Pane] {
			view.Locations = append(view.Locations, daemon.Location{
				Session: location.Session, Window: location.Window, Pane: location.Pane,
			})
		}
	}
	return view, nil
}
