package statusobserver

import (
	"context"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/status/service"
)

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
	revision, _ := s.core.Changes()
	values, err := s.core.ReadStatus(ctx)
	if err != nil {
		return daemon.View{}, err
	}
	reported := make(map[string]bool, values.Len())
	for ref := range values.All() {
		reported[ref.ID] = true
	}
	view := daemon.View{Values: values, Cursor: daemon.Cursor{Revision: revision}}
	for _, location := range s.locations {
		if reported[location.Pane] {
			view.Locations = append(view.Locations, daemon.Location{
				Session: location.Session, Window: location.Window, Pane: location.Pane,
			})
		}
	}
	return view, nil
}

// WatchView waits for evidence, topology or validation renewal without doing I/O
// or holding routing locks while waiting. The daemon owns the wait deadline.
func (s *Source) WatchView(ctx context.Context, after uint64) (daemon.View, error) {
	for {
		s.mu.Lock()
		revision, changes := s.core.Changes()
		if revision != after || ctx.Err() != nil || s.closed || s.failure != nil {
			view, err := s.readView(ctx)
			view.Cursor.Revision = revision
			s.mu.Unlock()
			return view, err
		}
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return daemon.View{}, ctx.Err()
		case <-s.ctx.Done():
			return daemon.View{}, daemon.ErrUnavailable
		case <-changes:
		}
	}
}
