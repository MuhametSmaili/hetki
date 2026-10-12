package daemon

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

type watchHandlerFixture struct {
	ctx      context.Context
	daemon   *daemon
	source   *watchSource
	listener *net.UnixListener
	path     string
}

func newWatchHandlerFixture(t *testing.T) watchHandlerFixture {
	t.Helper()
	client := fixtureClient(t, "watch-handlers")
	core, err := service.New(client.server)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, core.Close()) })

	source := &watchSource{core: core, waiting: make(chan struct{}, maxWatchers)}
	listener, cleanup, err := listen(client.path)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	logs, err := openDaemonLog(client.LogPath())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, logs.close()) })

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	d := &daemon{
		binding:  liveBinding(client.server),
		source:   source,
		cancel:   cancel,
		logs:     logs,
		watchers: make(chan struct{}, maxWatchers),
		info:     Info{PID: 1, Epoch: "0123456789abcdef0123456789abcdef"},
	}
	return watchHandlerFixture{ctx: ctx, daemon: d, source: source, listener: listener, path: client.path}
}

func (f watchHandlerFixture) open(t *testing.T, ctx context.Context) (net.Conn, <-chan struct{}) {
	t.Helper()
	require.NoError(t, f.listener.SetDeadline(time.Now().Add(time.Second)))
	client, err := net.DialTimeout("unix", f.path, time.Second)
	require.NoError(t, err)

	server, err := f.listener.AcceptUnix()
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.daemon.handle(ctx, server)
	}()
	return client, done
}

func (f watchHandlerFixture) disconnectBatch(t *testing.T, shutdown bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(f.ctx)
	var clients []net.Conn
	var handlers []<-chan struct{}
	defer func() {
		cancel()
		for _, client := range clients {
			_ = client.Close()
		}
		for _, done := range handlers {
			receive(t, done)
		}
	}()

	revision, _ := f.source.core.Changes()
	after := Cursor{Epoch: f.daemon.info.Epoch, Revision: revision}
	for range maxWatchers {
		client, done := f.open(t, ctx)
		clients = append(clients, client)
		handlers = append(handlers, done)
		require.NoError(t, client.SetDeadline(time.Now().Add(3*time.Second)))
		require.NoError(t, writeFrame(client, maxRequestBytes, request{
			Version: protocolVersion, Server: f.daemon.binding.Server, Action: "watch", After: &after,
		}))
		receive(t, f.source.waiting)
	}
	require.Len(t, f.daemon.watchers, maxWatchers)

	if shutdown {
		cancel()
	} else {
		for _, client := range clients {
			require.NoError(t, client.Close())
		}
	}
	for _, done := range handlers {
		receive(t, done)
	}

	require.Empty(t, f.daemon.watchers)
}

func TestWatchHandlersReclaimFullCapacityAfterRepeatedDisconnects(t *testing.T) {
	fixture := newWatchHandlerFixture(t)

	for range 32 {
		fixture.disconnectBatch(t, false)
	}

	require.Empty(t, fixture.daemon.watchers)
}

func TestWatchHandlersJoinAllReadersOnShutdown(t *testing.T) {
	fixture := newWatchHandlerFixture(t)

	fixture.disconnectBatch(t, true)

	require.Empty(t, fixture.daemon.watchers)
}
