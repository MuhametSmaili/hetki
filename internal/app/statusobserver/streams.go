// Package statusobserver coordinates native acquisition for application composition.
package statusobserver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
)

const maxSubscriptions = 128

var (
	ErrClosed            = errors.New("status streams closed")
	ErrStaleInventory    = errors.New("status streams require a newer inventory")
	ErrSubscriptionLimit = errors.New("status stream subscription limit reached")
)

// Scope identifies one uninterrupted decoder input. Generation changes on owner
// handoff, native lifetime change or reconnect; it is not a pane incarnation.
type Scope struct {
	Pane       string
	Root       observe.Process
	Foreground observe.Process
	Connection string
	Generation uint64
}

// Consumer is the application's terminal dispatcher, not a protocol adapter.
// Calls are serialized and must be bounded, nonblocking and must not reenter
// Streams. Output bytes are borrowed until return. Reset immediately revokes the
// pane's decoder and evidence and supplies its replacement scope. An empty
// Connection means no validated input is available; removal supplies only Pane.
type Consumer interface {
	Reset(scope Scope)
	Output(scope Scope, data []byte) error
}

type connection interface {
	Generation() string
	ReadOutput(context.Context, func(observe.PaneOutput) error, func()) error
	Close() error
}

type subscription struct {
	session    string
	connection connection
	done       chan struct{}
	active     bool
	err        error
	started    time.Time
}

type route struct {
	pane      observe.Pane
	sessions  []string
	available bool
	owner     *subscription
	scope     Scope
}

// Streams owns at most 128 native connections and their output readers. It has
// no timer, acquisition worker or raw-output queue. The coordinator calls
// Reconcile with newer shared inventories and owns retry scheduling. Close must
// be called even when the parent context is cancelled.
type Streams struct {
	operations    sync.Mutex
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	consumer      Consumer
	open          func(context.Context, string) (connection, error)
	closed        bool
	server        string
	wake          chan struct{}
	revision      uint64
	generation    uint64
	panes         map[string]*route
	subscriptions map[string]*subscription
	closeErr      error
	retries       map[string]retryState
	watching      atomic.Bool
}

// NewStreams binds a subscription owner to a validated server and parent lifetime.
// Connections are opened only when Reconcile supplies shared inventory.
func NewStreams(ctx context.Context, server observe.Server, consumer Consumer) (*Streams, error) {
	if consumer == nil {
		return nil, fmt.Errorf("status streams require a consumer")
	}

	if err := server.Validate(ctx); err != nil {
		return nil, err
	}

	return newStreams(ctx, server.Identity(), consumer, func(ctx context.Context, session string) (connection, error) {
		return observe.Open(ctx, server, session)
	}), nil
}

func newStreams(ctx context.Context, server string, consumer Consumer, open func(context.Context, string) (connection, error)) *Streams {
	ctx, cancel := context.WithCancel(ctx)
	return &Streams{
		ctx:           ctx,
		cancel:        cancel,
		consumer:      consumer,
		server:        server,
		wake:          make(chan struct{}, 1),
		open:          open,
		panes:         make(map[string]*route),
		subscriptions: make(map[string]*subscription),
		retries:       make(map[string]retryState),
	}
}

// Changes coalesces topology and connection-loss hints. It carries no evidence or bytes; the
// coordinator should refresh inventory before retrying. Close does not close it.
func (s *Streams) Changes() <-chan struct{} { return s.wake }

// Close cancels pending startup and readers, revokes all routes and joins every
// owned worker. It is safe to call concurrently with Reconcile or another Close.
func (s *Streams) Close() error {
	s.cancel()
	s.operations.Lock()
	defer s.operations.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.closeErr
	}

	s.closed = true
	for id := range s.panes {
		s.consumer.Reset(Scope{Pane: id})
	}
	s.panes = nil
	subscriptions := s.subscriptions
	s.subscriptions = nil
	s.retries = nil
	s.mu.Unlock()

	for _, sub := range subscriptions {
		s.closeErr = errors.Join(s.closeErr, stopSubscription(sub))
	}
	return s.closeErr
}

func stopSubscription(sub *subscription) error {
	err := sub.connection.Close()
	<-sub.done
	return errors.Join(err, sub.err)
}

func (s *Streams) read(sub *subscription) {
	defer close(sub.done)
	err := sub.connection.ReadOutput(s.ctx, func(output observe.PaneOutput) error {
		return s.deliver(sub, output)
	}, s.signalChange)
	err = errors.Join(err, sub.connection.Close())

	s.mu.Lock()
	defer s.mu.Unlock()
	unexpected := sub.active && s.ctx.Err() == nil
	if unexpected {
		sub.err = errors.Join(observe.ErrStreamLost, err)
		s.retryLater(sub.session, time.Since(sub.started) >= healthyInterval)
	}
	sub.active = false
	for _, pane := range s.panes {
		if pane.owner == sub {
			s.assign(pane, nil)
		}
	}
	if unexpected {
		s.signalChange()
	}
}

func (s *Streams) signalChange() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Streams) deliver(sub *subscription, output observe.PaneOutput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}

	pane := s.panes[output.Pane]
	if pane == nil || pane.owner != sub || !sub.active {
		return nil
	}

	return s.consumer.Output(pane.scope, output.Data)
}
