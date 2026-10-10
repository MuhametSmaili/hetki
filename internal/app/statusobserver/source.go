package statusobserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/protocols"
	"github.com/MSmaili/hetki/internal/status/service"
)

// Selection is an immutable source choice made by application profile matching.
// Zero selects protocol reports. A screen choice requires a generation and bounded
// evaluator; changing its rules or region requires a new profile generation.
type Selection struct {
	Profile  service.Profile
	Region   observe.Region
	Evaluate func(string) (status.State, error)
}

// Source owns the watch, streams and cache for one server incarnation. Reads are
// cache-only; Close cancels and joins acquisition before closing the cache.
// Bare output is not evidence; only the selected adapter can update status.
type Source struct {
	mu sync.Mutex

	server           string
	epoch            string
	generation       uint64
	revision         uint64
	nativeRevision   uint64
	protocolSequence uint64
	validatedUntil   time.Time
	panes            map[string]sourcePane

	core    *service.Service
	streams *Streams
	screens *Screens

	selectProfile func(observe.Pane) Selection
	report        func(error)

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	failure error
	closed  bool

	closeOnce sync.Once
	closeErr  error
}

// NewSource starts one owned watch. A nil selector chooses no screen profiles.
// Selection and diagnostic callbacks run outside routing locks and must remain
// bounded; they must not call Close, which joins their own watch. The caller must
// Close the source even after cancelling its context.
func NewSource(
	ctx context.Context,
	server observe.Server,
	sampler *observe.Sampler,
	selectProfile func(observe.Pane) Selection,
	report func(error),
) (*Source, error) {
	if sampler == nil || report == nil {
		return nil, fmt.Errorf("status source requires sampler and error reporter")
	}

	core, err := service.New(server.Identity())
	if err != nil {
		return nil, err
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.Join(err, core.Close())
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &Source{
		server:        server.Identity(),
		epoch:         hex.EncodeToString(nonce[:]),
		panes:         make(map[string]sourcePane),
		core:          core,
		selectProfile: selectProfile,
		report:        report,
		ctx:           ctx,
		cancel:        cancel,
		done:          make(chan struct{}),
	}

	s.screens, err = NewScreens(sampler, core, report)
	if err != nil {
		cancel()
		return nil, errors.Join(err, core.Close())
	}

	s.streams, err = NewStreams(ctx, server, s)
	if err != nil {
		cancel()
		return nil, errors.Join(err, core.Close())
	}

	go s.run(sampler)
	return s, nil
}

func (s *Source) run(sampler *observe.Sampler) {
	defer close(s.done)

	err := watch(s.ctx, sampler, s.streams, s.screens, s.prepare, s.publish)
	if s.ctx.Err() != nil {
		err = unexpectedWatchError(err)
	}

	s.cancel()
	err = errors.Join(err, s.streams.Close())

	s.mu.Lock()
	s.failure = errors.Join(s.failure, err)
	failure := s.failure
	s.mu.Unlock()

	if failure != nil {
		s.report(failure)
	}
}

// Watch can join teardown failures with cancellation. Keep those failures while
// removing only the expected cancellation leaves after our context ends.
func unexpectedWatchError(err error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var result error
		for _, child := range joined.Unwrap() {
			result = errors.Join(result, unexpectedWatchError(child))
		}
		return result
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}

	return err
}

// ReadStatus returns cached evidence or the terminal acquisition failure.
// It is safe to call concurrently with Close and stream callbacks.
func (s *Source) ReadStatus(ctx context.Context) (status.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return status.Snapshot{}, err
	}

	s.mu.Lock()
	failure, closed := s.failure, s.closed
	s.mu.Unlock()

	if closed {
		return status.Snapshot{}, service.ErrClosed
	}

	if failure != nil {
		return status.Snapshot{}, failure
	}

	return s.core.ReadStatus(ctx)
}

// Close cancels and joins all acquisition before releasing the cache. Repeated
// calls return the same result, including any terminal acquisition failure.
func (s *Source) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.done

		s.mu.Lock()
		s.closed = true
		s.closeErr = errors.Join(s.failure, s.core.Close())
		s.panes = nil
		s.mu.Unlock()
	})
	return s.closeErr
}

// Reset is called synchronously before bytes from a new stream scope. It never
// performs native I/O or logging while Streams holds its routing lock.
func (s *Source) Reset(scope Scope) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, found := s.panes[scope.Pane]
	if !found || s.closed {
		return
	}

	if row.protocol != nil && row.stream != scope {
		row.protocol.resetStream(row.stream, scope, row.facts.Dead)
	}
	row.stream = scope
	row.pane.Scope = row.serviceScope()

	s.revision++
	if err := s.core.UpdateScope(s.revision, row.pane.Ref, row.pane.Scope); err != nil {
		s.failure = errors.Join(s.failure, err)
		s.cancel()
	}

	s.panes[scope.Pane] = row
	s.bindProtocol(row)
}

// Output dispatches attributed bytes to the selected source. Protocol framing
// and reduction are synchronous. Screen-selected panes track shell boundaries
// independently while output schedules a checked capture for activity evidence.
func (s *Source) Output(scope Scope, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, found := s.panes[scope.Pane]
	if s.closed || !found || row.stream != scope || row.pane.Scope.Connection == "" {
		return nil
	}

	if row.selection.Profile.Source == service.Screen {
		s.screens.Dirty(scope.Pane)
	}

	if row.protocol == nil {
		row.protocol = &protocolBinding{
			pane: protocols.New(protocols.Options{ProgramStatus: row.pane.EvidenceSource() == service.Protocol}),
		}
		s.panes[scope.Pane] = row
	}
	return s.protocolOutput(row, data)
}
