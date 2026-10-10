package statusobserve

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

type consumed struct {
	scope Scope
	text  string
}

type recorder struct {
	mu      sync.Mutex
	outputs []consumed
	resets  []string
	failure error
}

func (r *recorder) Reset(pane string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resets = append(r.resets, pane)
}

func (r *recorder) Output(scope Scope, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs = append(r.outputs, consumed{scope: scope, text: string(data)})
	return r.failure
}

func (r *recorder) snapshot() ([]consumed, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]consumed(nil), r.outputs...), append([]string(nil), r.resets...)
}

type delivery struct {
	output observe.PaneOutput
	ack    chan struct{}
}

type fakeConnection struct {
	id      string
	input   chan delivery
	failure chan error
	stop    chan struct{}
	joined  chan struct{}
	once    sync.Once
}

func fakeStream(id string) *fakeConnection {
	return &fakeConnection{
		id: id, input: make(chan delivery), failure: make(chan error, 1),
		stop: make(chan struct{}), joined: make(chan struct{}),
	}
}

func (c *fakeConnection) Generation() string { return c.id }

func (c *fakeConnection) Close() error {
	c.once.Do(func() { close(c.stop) })
	return nil
}

func (c *fakeConnection) ReadOutput(ctx context.Context, emit func(observe.PaneOutput) error) error {
	defer close(c.joined)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stop:
			return observe.ErrStreamLost
		case err := <-c.failure:
			return err
		case item := <-c.input:
			err := emit(item.output)
			close(item.ack)
			if err != nil {
				return err
			}
		}
	}
}

func (c *fakeConnection) send(t *testing.T, pane, text string) {
	t.Helper()
	item := delivery{output: observe.PaneOutput{Pane: pane, Data: []byte(text)}, ack: make(chan struct{})}
	select {
	case c.input <- item:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not receive fixture output")
	}
	await(t, item.ack)
}

func await(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for owned worker")
	}
}

func fixtureTopology(revision uint64, sessions ...string) topology {
	return topology{server: "server", revision: revision, panes: map[string]paneFacts{
		"%1": {pane: observe.Pane{
			ID: "%1", RootPID: 100, TTY: "/dev/fixture", Command: "app", Columns: 80, Rows: 30,
			Root: observe.Process{PID: 100, Birth: "root"}, Foreground: observe.Process{PID: 101, Birth: "foreground"},
		}, sessions: sessions, valid: true},
	}}
}

func streamFixture(t *testing.T) (*Streams, *recorder, map[string]*fakeConnection) {
	t.Helper()
	record := &recorder{}
	connections := make(map[string]*fakeConnection)
	serial := 0
	streams := newStreams(t.Context(), "server", record, func(_ context.Context, session string) (connection, error) {
		serial++
		conn := fakeStream(fmt.Sprintf("%s-%d", session, serial))
		connections[session] = conn
		return conn, nil
	})
	t.Cleanup(func() { _ = streams.Close() })
	return streams, record, connections
}

func TestLinkedPaneHasOneStableInputOwner(t *testing.T) {
	streams, record, connections := streamFixture(t)
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$2")))
	connections["$2"].send(t, "%1", "first")
	_, initialResets := record.snapshot()

	err := streams.reconcile(t.Context(), fixtureTopology(2, "$1", "$2"))
	connections["$1"].send(t, "%1", "duplicate")
	connections["$2"].send(t, "%1", "same")
	connections["$2"].send(t, "%1", "same")
	outputs, resets := record.snapshot()

	require.NoError(t, err)
	require.Len(t, outputs, 3)
	require.Equal(t, "first", outputs[0].text)
	require.Equal(t, "same", outputs[1].text)
	require.Equal(t, "same", outputs[2].text)
	require.Equal(t, outputs[0].scope, outputs[2].scope)
	require.Equal(t, initialResets, resets)
}

func TestPaneHandoffRevokesOldOwnerBeforeAcceptingNewBytes(t *testing.T) {
	streams, record, connections := streamFixture(t)
	initial := fixtureTopology(1, "$1", "$2")
	initial.panes["%2"] = paneFacts{pane: observe.Pane{ID: "%2", Dead: true}, sessions: []string{"$1"}}
	require.NoError(t, streams.reconcile(t.Context(), initial))
	connections["$1"].send(t, "%1", "before")
	before, oldResets := record.snapshot()
	next := fixtureTopology(2, "$2")
	next.panes["%2"] = initial.panes["%2"]

	err := streams.reconcile(t.Context(), next)
	connections["$1"].send(t, "%1", "delayed old owner")
	connections["$2"].send(t, "%1", "after")
	after, resets := record.snapshot()

	require.NoError(t, err)
	require.Len(t, after, 2)
	require.Equal(t, "after", after[1].text)
	require.Greater(t, after[1].scope.Generation, before[0].scope.Generation)
	require.NotEqual(t, before[0].scope.Connection, after[1].scope.Connection)
	require.Len(t, resets, len(oldResets)+1)
}

func TestConnectionLossResetsImmediatelyAndReconnectCannotReviveScope(t *testing.T) {
	streams, record, connections := streamFixture(t)
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
	connections["$1"].send(t, "%1", "before")
	before, oldResets := record.snapshot()
	failed := connections["$1"]

	failed.failure <- observe.ErrStreamLost
	await(t, streams.Changes())
	_, lossResets := record.snapshot()
	err := streams.reconcile(t.Context(), fixtureTopology(2, "$1"))
	connections["$1"].send(t, "%1", "fresh")
	after, _ := record.snapshot()

	require.Len(t, lossResets, len(oldResets)+1)
	require.ErrorIs(t, err, observe.ErrStreamLost)
	await(t, failed.joined)
	require.Len(t, after, 2)
	require.Greater(t, after[1].scope.Generation, before[0].scope.Generation)
	require.NotEqual(t, before[0].scope.Connection, after[1].scope.Connection)
}

func TestNativeScopeChangesResetButResizePreservesStream(t *testing.T) {
	cases := []struct {
		name   string
		change func(*paneFacts)
		reset  bool
		accept bool
	}{
		{"resize", func(p *paneFacts) { p.pane.Columns++ }, false, true},
		{"foreground", func(p *paneFacts) { p.pane.Foreground.Birth = "new" }, true, true},
		{"root", func(p *paneFacts) { p.pane.Root.Birth = "new" }, true, true},
		{"command", func(p *paneFacts) { p.pane.Command = "other" }, true, true},
		{"dead", func(p *paneFacts) { p.pane.Dead = true }, true, false},
		{"unavailable", func(p *paneFacts) { p.valid = false }, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			streams, record, connections := streamFixture(t)
			require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
			connections["$1"].send(t, "%1", "before")
			_, before := record.snapshot()
			next := fixtureTopology(2, "$1")
			facts := next.panes["%1"]
			tc.change(&facts)
			next.panes["%1"] = facts

			err := streams.reconcile(t.Context(), next)
			connections["$1"].send(t, "%1", "after")
			outputs, resets := record.snapshot()

			require.NoError(t, err)
			require.Equal(t, tc.reset, len(resets) > len(before))
			require.Equal(t, tc.accept, len(outputs) == 2)
		})
	}
}

func TestCloseCancelsStartupAndJoinsReaders(t *testing.T) {
	streams, _, connections := streamFixture(t)
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
	started := make(chan struct{})
	streams.open = func(ctx context.Context, _ string) (connection, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	result := make(chan error, 1)
	go func() { result <- streams.reconcile(t.Context(), fixtureTopology(2, "$1", "$2")) }()
	await(t, started)

	err := streams.Close()

	require.NoError(t, err)
	await(t, connections["$1"].joined)
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("reconciliation did not terminate")
	}
	require.NoError(t, streams.Close())
	require.ErrorIs(t, streams.reconcile(t.Context(), fixtureTopology(3, "$1")), ErrClosed)
}

func TestConsumerFailureInvalidatesTheConnection(t *testing.T) {
	streams, record, connections := streamFixture(t)
	failure := errors.New("decoder could not preserve continuity")
	record.failure = failure
	require.NoError(t, streams.reconcile(t.Context(), fixtureTopology(1, "$1")))
	_, before := record.snapshot()

	connections["$1"].send(t, "%1", "bad")
	await(t, streams.Changes())
	_, after := record.snapshot()
	err := streams.Close()

	require.Len(t, after, len(before)+1)
	require.ErrorIs(t, err, failure)
}
