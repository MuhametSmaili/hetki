package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

// readFirst subscribes, reads the first view and closes the subscription.
func readFirst(ctx context.Context, c *Client) (status.Snapshot, error) {
	subscription, err := c.Subscribe(ctx)
	if err != nil {
		return status.Snapshot{}, err
	}
	defer subscription.Close()

	stop := context.AfterFunc(ctx, func() {
		_ = subscription.Close()
	})
	defer stop()

	view, err := subscription.Next()
	if ctx.Err() != nil {
		return status.Snapshot{}, ctx.Err()
	}
	return view.Values, err
}

type changingSource struct {
	mu      sync.Mutex
	view    View
	err     error
	changes chan struct{}
}

func newChangingSource(view View, err error) *changingSource {
	return &changingSource{view: view, err: err, changes: make(chan struct{})}
}

func (s *changingSource) Changes() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes
}

func (s *changingSource) ReadView(context.Context) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view, s.err
}

func (s *changingSource) Close() error { return nil }

func (s *changingSource) set(view View, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view, s.err = view, err
	close(s.changes)
	s.changes = make(chan struct{})
}

func (s *changingSource) factory() SourceFactory {
	return func(context.Context, func(error)) (Source, error) { return s, nil }
}

func testView(t *testing.T, server string, state status.State) View {
	t.Helper()
	pane := status.PaneRef{Server: server, ID: "%1", Generation: "birth"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: state, ValidUntil: time.Now().UTC().Add(time.Minute)},
	})
	require.NoError(t, err)
	return View{Values: values, Locations: []Location{{Session: "$1", Window: "@1", Pane: "%1"}}}
}

func subscribe(t *testing.T, c *Client) *Subscription {
	t.Helper()
	subscription, err := c.Subscribe(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = subscription.Close()
	})
	return subscription
}

func paneState(t *testing.T, view View, server string) status.State {
	t.Helper()
	return view.Values.Lookup(status.PaneRef{Server: server, ID: "%1", Generation: "birth"}).State
}

func TestSubscriptionSendsCurrentViewThenEachChange(t *testing.T) {
	c := fixtureClient(t, "subscribe")
	source := newChangingSource(testView(t, c.server, status.Idle), nil)
	startFixture(t, c, liveBinding(c.server), source.factory())
	subscription := subscribe(t, c)

	first, firstErr := subscription.Next()
	source.set(testView(t, c.server, status.Working), nil)
	second, secondErr := subscription.Next()

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Equal(t, status.Idle, paneState(t, first, c.server))
	require.Equal(t, status.Working, paneState(t, second, c.server))
	require.Equal(t, []Location{{Session: "$1", Window: "@1", Pane: "%1"}}, second.Locations)
}

func TestSubscriptionStaysOpenAfterUnavailableView(t *testing.T) {
	c := fixtureClient(t, "subscribe-unavailable")
	source := newChangingSource(View{}, errors.New("no inventory yet"))
	startFixture(t, c, liveBinding(c.server), source.factory())
	subscription := subscribe(t, c)

	_, firstErr := subscription.Next()
	source.set(testView(t, c.server, status.Idle), nil)
	recovered, recoveredErr := subscription.Next()

	require.ErrorIs(t, firstErr, ErrUnavailable)
	require.NoError(t, recoveredErr)
	require.Equal(t, status.Idle, paneState(t, recovered, c.server))
}

func TestSubscriberLimitKeepsControlRequestsAvailable(t *testing.T) {
	c := fixtureClient(t, "subscribe-limit")
	source := newChangingSource(testView(t, c.server, status.Idle), nil)
	startFixture(t, c, liveBinding(c.server), source.factory())
	subscriptions := make([]*Subscription, 0, maxSubscribers)
	for range maxSubscribers {
		subscription := subscribe(t, c)
		_, err := subscription.Next()
		require.NoError(t, err)
		subscriptions = append(subscriptions, subscription)
	}

	_, rejected := subscribe(t, c).Next()
	_, pingErr := c.Ping(t.Context())
	for _, subscription := range subscriptions {
		require.NoError(t, subscription.Close())
	}
	source.set(testView(t, c.server, status.Working), nil)

	require.Error(t, rejected)
	require.NoError(t, pingErr)
	require.Eventually(t, func() bool {
		_, err := readFirst(t.Context(), c)
		return err == nil
	}, time.Second, 10*time.Millisecond)
}

func TestSubscriptionEndsWhenDaemonStops(t *testing.T) {
	c := fixtureClient(t, "subscribe-stop")
	source := newChangingSource(testView(t, c.server, status.Idle), nil)
	startFixture(t, c, liveBinding(c.server), source.factory())
	subscription := subscribe(t, c)
	_, err := subscription.Next()
	require.NoError(t, err)

	require.NoError(t, c.Stop(t.Context()))
	_, ended := subscription.Next()

	require.Error(t, ended)
	require.NotErrorIs(t, ended, ErrUnavailable)
}
