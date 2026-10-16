package statusobserver

import (
	"math/rand/v2"
	"time"
)

const healthyInterval = 10 * time.Second

type retryState struct {
	attempt uint8
	after   time.Time
}

func retryDelay(attempt uint8) time.Duration {
	ceiling := 500 * time.Millisecond << min(attempt-1, 4)
	floor := ceiling * 4 / 5
	return floor + time.Duration(rand.Int64N(int64(ceiling-floor)+1))
}

// Caller holds mu. Short-lived successful connections retain their failure
// history so an accept-then-disconnect loop cannot defeat backoff.
func (s *Streams) retryLater(session string, healthy bool) {
	retry := s.retries[session]
	if healthy {
		retry.attempt = 0
	}

	retry.attempt = min(retry.attempt+1, 5)
	retry.after = time.Now().Add(retryDelay(retry.attempt))
	s.retries[session] = retry
}

func (s *Streams) nextRetry() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	var next time.Time
	for session, retry := range s.retries {
		if sub := s.subscriptions[session]; sub != nil && sub.active {
			continue
		}

		if next.IsZero() || retry.after.Before(next) {
			next = retry.after
		}
	}
	return next
}

// Failed acquisition revokes input immediately but retains connections so a
// transient query failure need not cause a reconnect storm.
func (s *Streams) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pane := range s.panes {
		pane.available = false
		if pane.owner != nil {
			s.assign(pane, nil)
		}
	}
}
