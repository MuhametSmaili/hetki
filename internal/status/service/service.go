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

	"github.com/MuhametSmaili/hetki/internal/status"
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
	evidence   status.Snapshot
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

	if err := s.prepareRead(ctx); err != nil {
		return status.Snapshot{}, err
	}
	return s.snapshot, nil
}

// ReadEvidence returns the values without the lease, and the lease. ReadStatus
// applies the lease. A transport sends the lease separately, so that a renewal
// does not change each value.
func (s *Service) ReadEvidence(ctx context.Context) (status.Snapshot, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.prepareRead(ctx); err != nil {
		return status.Snapshot{}, time.Time{}, err
	}
	return s.evidence, s.validUntil, nil
}

// prepareRead checks that the service is open and rebuilds stale snapshots. Caller holds mu.
func (s *Service) prepareRead(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrClosed
	}

	if s.dirty {
		return s.refreshSnapshot()
	}
	return nil
}

func (s *Service) refreshSnapshot() error {
	values := make(map[status.PaneRef]status.Value, len(s.panes))
	for _, row := range s.panes {
		if row.value.State != status.Unreported {
			values[row.pane.Ref] = row.value
		}
	}

	evidence, err := status.NewSnapshot(values)
	if err != nil {
		return err
	}

	s.evidence = evidence
	s.snapshot = evidence.Leased(s.validUntil)
	s.dirty = false
	return nil
}

func (s *Service) changed() {
	s.invalidateSnapshot()
	s.version++
	close(s.changes)
	s.changes = make(chan struct{})
}

func (s *Service) invalidateSnapshot() {
	s.evidence = status.Snapshot{}
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
	s.evidence = status.Snapshot{}
	s.snapshot = status.Snapshot{}
	return nil
}
