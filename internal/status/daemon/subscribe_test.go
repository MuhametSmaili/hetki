package daemon

import (
	"context"
	"errors"
	"net"
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
	return View{Values: values, Locations: []Location{{Session: "$1", Window: "@1", Pane: "%1"}}, Lease: time.Now().Add(time.Hour)}
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

func rawSubscribe(t *testing.T, c *Client) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", c.path)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})
	require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
	require.NoError(t, writeFrame(conn, maxRequestBytes, request{Version: protocolVersion, Server: c.server, Action: "subscribe"}))
	return conn
}

func TestLeaseRenewalSendsOnlyMovedDeadlines(t *testing.T) {
	c := fixtureClient(t, "renewal")
	view := testView(t, c.server, status.Working)
	source := newChangingSource(view, nil)
	startFixture(t, c, liveBinding(c.server), source.factory())
	conn := rawSubscribe(t, c)
	var full, renewal reply
	require.NoError(t, readFrame(conn, maxReplyBytes, &full))

	renewed := view
	renewed.Lease = view.Lease.Add(time.Minute)
	source.set(renewed, nil)
	require.NoError(t, readFrame(conn, maxReplyBytes, &renewal))

	require.False(t, full.Renewal)
	require.Len(t, full.Entries, 1)
	require.True(t, renewal.Renewal)
	require.Empty(t, renewal.Entries, "no evidence deadline moved")
	require.Empty(t, renewal.Locations)
	require.True(t, renewed.Lease.Equal(renewal.Lease))
}

func TestRenewalCarriesMovedEvidenceDeadlines(t *testing.T) {
	c := fixtureClient(t, "renewal-deadline")
	view := testView(t, c.server, status.Working)
	source := newChangingSource(view, nil)
	startFixture(t, c, liveBinding(c.server), source.factory())
	conn := rawSubscribe(t, c)
	var full, renewal reply
	require.NoError(t, readFrame(conn, maxReplyBytes, &full))
	pane := status.PaneRef{Server: c.server, ID: "%1", Generation: "birth"}
	extended := view.Values.Lookup(pane)
	extended.ValidUntil = extended.ValidUntil.Add(time.Minute)
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: extended})
	require.NoError(t, err)

	source.set(View{Values: values, Locations: view.Locations, Lease: view.Lease}, nil)
	require.NoError(t, readFrame(conn, maxReplyBytes, &renewal))

	require.True(t, renewal.Renewal)
	require.Len(t, renewal.Entries, 1)
	require.True(t, extended.ValidUntil.Equal(renewal.Entries[0].ValidUntil))
}

func TestSubscriptionAppliesRenewalsToTheLastView(t *testing.T) {
	c := fixtureClient(t, "renewal-client")
	view := testView(t, c.server, status.Working)
	view.Lease = time.Now().Add(10 * time.Second) // Earlier than the evidence deadline.
	source := newChangingSource(view, nil)
	startFixture(t, c, liveBinding(c.server), source.factory())
	subscription := subscribe(t, c)
	pane := status.PaneRef{Server: c.server, ID: "%1", Generation: "birth"}
	first, err := subscription.Next()
	require.NoError(t, err)

	renewed := view
	renewed.Lease = view.Lease.Add(time.Hour)
	source.set(renewed, nil)
	second, err := subscription.Next()

	require.NoError(t, err)
	require.True(t, view.Lease.Equal(first.Values.Lookup(pane).ValidUntil), "the lease limits activity")
	require.True(t, view.Values.Lookup(pane).ValidUntil.Equal(second.Values.Lookup(pane).ValidUntil), "the evidence deadline limits it now")
	require.Equal(t, view.Locations, second.Locations)
}

func TestSubscriptionRejectsInvalidRenewals(t *testing.T) {
	cases := map[string][]reply{
		"renewal before a full view": {{Renewal: true}},
		"renewal for an unknown pane": {
			{},
			{Renewal: true, Entries: []entry{{ID: "%9", Generation: "birth"}}},
		},
	}
	for name, frames := range cases {
		t.Run(name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			subscription := &Subscription{client: &Client{server: "server"}, conn: client}
			go func() {
				defer server.Close()
				for _, frame := range frames {
					frame.Version, frame.Server, frame.PID, frame.Epoch = protocolVersion, "server", 1, "0123456789abcdef0123456789abcdef"
					if writeFrame(server, maxReplyBytes, frame) != nil {
						return
					}
				}
			}()

			var err error
			for range frames {
				_, err = subscription.Next()
			}

			require.ErrorContains(t, err, "renewal")
		})
	}
}
