package daemon

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOldWireRejectsTargetedStopWithoutFallback(t *testing.T) {
	client := fixtureClient(t, "legacy-stop")
	listener, cleanup, err := listen(client.path)
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, listener.SetDeadline(time.Now().Add(time.Second)))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.requestStop(ctx, "0123456789abcdef0123456789abcdef") }()
	conn, err := listener.AcceptUnix()
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
	// Freeze the actual v1 request shape, using its strict production decoder.
	var legacy struct {
		Version int     `json:"version"`
		Server  string  `json:"server"`
		Action  string  `json:"action"`
		After   *Cursor `json:"after,omitempty"`
	}

	decodeErr := readFrame(conn, maxRequestBytes, &legacy)
	require.NoError(t, conn.Close())
	stopErr := receive(t, done)

	require.ErrorContains(t, decodeErr, `unknown field "expected_epoch"`)
	require.Error(t, stopErr)
	// Any fallback would need another connection. The completed call made none.
	require.NoError(t, listener.SetDeadline(time.Now().Add(20*time.Millisecond)))
	unexpected, acceptErr := listener.AcceptUnix()
	if unexpected != nil {
		_ = unexpected.Close()
	}
	require.Error(t, acceptErr)
}

func lifecycleConnection(t *testing.T, fixture watchHandlerFixture) (*net.UnixConn, net.Conn) {
	t.Helper()
	require.NoError(t, fixture.listener.SetDeadline(time.Now().Add(time.Second)))
	client, err := net.DialTimeout("unix", fixture.path, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	server, err := fixture.listener.AcceptUnix()
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })
	require.NoError(t, server.SetDeadline(time.Now().Add(time.Second)))
	require.NoError(t, client.SetDeadline(time.Now().Add(time.Second)))
	return server, client
}

func TestAcceptedStopSurvivesAcknowledgmentFailure(t *testing.T) {
	fixture := newWatchHandlerFixture(t)
	server, client := lifecycleConnection(t, fixture)
	require.NoError(t, client.Close())
	req := request{Action: "stop", ExpectedEpoch: fixture.daemon.info.Epoch}
	row, ok := fixture.daemon.respond(fixture.ctx, req)
	require.True(t, ok)

	err := fixture.daemon.publish(fixture.ctx, server, req, row)

	require.Error(t, err)
	require.ErrorIs(t, fixture.ctx.Err(), context.Canceled)
}

func TestAcceptedStopBlocksReadinessUntilCancellation(t *testing.T) {
	fixture := newWatchHandlerFixture(t)
	stopConn, stopPeer := lifecycleConnection(t, fixture)
	pingConn, pingPeer := lifecycleConnection(t, fixture)
	ping := request{Action: "ping"}
	ready, ok := fixture.daemon.respond(fixture.ctx, ping)
	require.True(t, ok)
	stop := request{Action: "stop", ExpectedEpoch: fixture.daemon.info.Epoch}
	ack, ok := fixture.daemon.respond(fixture.ctx, stop)
	require.True(t, ok)

	accepted := make(chan struct{})
	allowCancellation := make(chan struct{})
	release := sync.OnceFunc(func() { close(allowCancellation) })
	gateCtx, cancelGate := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelGate()
	cancelDaemon := fixture.daemon.cancel
	fixture.daemon.cancel = func() {
		close(accepted)
		select {
		case <-allowCancellation:
		case <-gateCtx.Done():
		}
		cancelDaemon()
	}

	stopDone := make(chan error, 1)
	go func() {
		defer close(stopDone)
		stopDone <- fixture.daemon.publish(fixture.ctx, stopConn, stop, ack)
	}()
	defer func() {
		release()
		receive(t, stopDone)
	}()
	var response reply
	require.NoError(t, readFrame(stopPeer, maxReplyBytes, &response))
	receive(t, accepted)

	// Hold accepted shutdown before cancellation while an earlier Ping tries to
	// publish. The read deadline bounds the assertion, not the overlap itself.
	pingStarted := make(chan struct{})
	pingDone := make(chan error, 1)
	go func() {
		defer close(pingDone)
		close(pingStarted)
		pingDone <- fixture.daemon.publish(fixture.ctx, pingConn, ping, ready)
	}()
	defer func() {
		release()
		receive(t, pingDone)
	}()
	receive(t, pingStarted)
	require.NoError(t, pingPeer.SetReadDeadline(time.Now().Add(100*time.Millisecond)))

	err := readFrame(pingPeer, maxReplyBytes, &response)

	var timeout net.Error
	require.ErrorAs(t, err, &timeout)
	require.True(t, timeout.Timeout(), "readiness must stay blocked after stop acceptance")
	require.NoError(t, fixture.ctx.Err(), "this must not merely test an already-canceled context")
	release()
	require.NoError(t, receive(t, stopDone))
	require.ErrorIs(t, receive(t, pingDone), context.Canceled)
}
