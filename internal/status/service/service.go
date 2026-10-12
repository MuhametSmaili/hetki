// Package service selects application evidence and owns a bounded status
// cache. Callers supply validated inventory and normalized observations, not I/O.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MSmaili/hetki/internal/status"
)

var (
	ErrStale  = errors.New("stale status evidence or inventory")
	ErrClosed = errors.New("status service closed")
)

// Service is concurrency-safe and owns no workers or I/O resources.
type Service struct {
	mu         sync.Mutex
	server     string
	panes      map[string]record
	revision   uint64
	generation uint64
	validUntil time.Time
	snapshot   status.Snapshot
	dirty      bool
	closed     bool
	version    uint64
	changes    chan struct{}
}

type record struct {
	pane       Pane
	generation uint64
	sequence   uint64
	value      status.Value
}

func New(server string) (*Service, error) {
	if server == "" || len(server) > maxIdentityBytes {
		return nil, fmt.Errorf("status service requires a bounded server identity")
	}

	return &Service{
		server:  strings.Clone(server),
		version: 1,
		changes: make(chan struct{}),
	}, nil
}

// ReadStatus performs no collection. Unchanged reads share an immutable snapshot;
// consumers use Value.At to evaluate its validation deadline.
func (s *Service) ReadStatus(ctx context.Context) (status.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return status.Snapshot{}, err
	}

	if s.closed {
		return status.Snapshot{}, ErrClosed
	}

	if s.dirty {
		if err := s.refreshSnapshot(); err != nil {
			return status.Snapshot{}, err
		}
	}

	return s.snapshot, nil
}

func (s *Service) refreshSnapshot() error {
	values := make(map[status.PaneRef]status.Value, len(s.panes))
	for _, row := range s.panes {
		if row.value.State == status.Unreported {
			continue
		}

		value := row.value
		if value.State == status.Working || value.State == status.Blocked || value.State == status.Idle || value.Outcome != status.NoOutcome {
			value.ValidUntil = earlierDeadline(value.ValidUntil, s.validUntil)
		}
		values[row.pane.Ref] = value
	}

	snapshot, err := status.NewSnapshot(values)
	if err != nil {
		return err
	}

	s.snapshot = snapshot
	s.dirty = false
	return nil
}

func earlierDeadline(evidence, inventory time.Time) time.Time {
	if evidence.IsZero() || inventory.Before(evidence) {
		return inventory
	}

	return evidence
}

func (s *Service) changed() {
	s.invalidateSnapshot()
	s.version++
	close(s.changes)
	s.changes = make(chan struct{})
}

func (s *Service) invalidateSnapshot() {
	s.snapshot = status.Snapshot{}
	s.dirty = true
}

// Changes returns an atomic revision and broadcast signal. Inventory renewals
// wake readers to refresh deadlines; identical reports and capture-deadline-only
// extensions do not. Callers must read again after wakeup, not replay events.
func (s *Service) Changes() (uint64, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.version, s.changes
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	s.version++
	close(s.changes)
	s.panes = nil
	s.snapshot = status.Snapshot{}
	return nil
}
