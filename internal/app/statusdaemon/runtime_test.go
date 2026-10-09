package statusdaemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

type fixtureSource struct {
	read  func(context.Context) (status.Snapshot, error)
	close func() error
}

func (s fixtureSource) ReadStatus(ctx context.Context) (status.Snapshot, error) {
	return s.read(ctx)
}

func (s fixtureSource) Close() error {
	return s.close()
}

func fixtureClient(t testing.TB, server string) *Client {
	t.Helper()

	dir, err := os.MkdirTemp("", "hs-")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", dir)

	c, err := Connect(server + "-" + filepath.Base(dir))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(c.path)
		_ = os.Remove(c.path + ".lock")
		_ = os.Remove(c.path + ".old")
		_ = os.Remove(c.LogPath())
		_ = os.Remove(c.LogPath() + ".1")
	})

	return c
}

func startFixture(t testing.TB, c *Client, binding Binding, start func(context.Context) (Source, error)) <-chan error {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, binding, start)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Error("daemon did not join workers")
		}
	})
	require.NoError(t, c.waitReady(ctx, nil))

	return done
}

func receive[T any](t testing.TB, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for fixture")
		var zero T
		return zero
	}
}

func staticSource(snapshot status.Snapshot) func(context.Context) (Source, error) {
	return func(context.Context) (Source, error) {
		return fixtureSource{
			read:  func(context.Context) (status.Snapshot, error) { return snapshot, nil },
			close: func() error { return nil },
		}, nil
	}
}

func liveBinding(server string) Binding {
	return Binding{Server: server, Alive: func(context.Context) bool { return true }}
}

func TestSnapshotRoundTripPreservesIdentityFreshnessAndAbsence(t *testing.T) {
	c := fixtureClient(t, "server-birth")
	deadline := time.Now().UTC().Add(time.Minute)
	values := map[status.PaneRef]status.Value{}
	for _, state := range []status.State{status.Working, status.Idle, status.Blocked, status.Unknown, status.Withdrawn} {
		values[status.PaneRef{Server: c.server, ID: string(state), Generation: "birth"}] = status.Value{State: state, ValidUntil: deadline}
	}
	snapshot, err := status.NewSnapshot(values)
	require.NoError(t, err)
	startFixture(t, c, liveBinding(c.server), staticSource(snapshot))

	got, err := c.ReadStatus(context.Background())

	require.NoError(t, err)
	for pane, value := range values {
		require.Equal(t, value, got.Lookup(pane))
	}
	require.Equal(t, status.Unreported, got.At(status.PaneRef{Server: c.server, ID: "missing", Generation: "birth"}, deadline))
	require.Equal(t, status.Unknown, got.At(status.PaneRef{Server: c.server, ID: "working", Generation: "birth"}, deadline))
}

func TestShutdownCancelsReadsBeforeClosingSource(t *testing.T) {
	c := fixtureClient(t, "shutdown")
	reading := make(chan struct{})
	canceled := make(chan struct{})
	closed := make(chan bool, 1)
	done := startFixture(t, c, liveBinding(c.server), func(ctx context.Context) (Source, error) {
		return fixtureSource{
			read: func(ctx context.Context) (status.Snapshot, error) {
				close(reading)
				<-ctx.Done()
				close(canceled)
				return status.Snapshot{}, ctx.Err()
			},
			close: func() error {
				select {
				case <-canceled:
					closed <- ctx.Err() != nil
				default:
					closed <- false
				}

				return nil
			},
		}, nil
	})

	readDone := make(chan error, 1)
	go func() {
		_, err := c.ReadStatus(context.Background())
		readDone <- err
	}()
	receive(t, reading)

	err := c.Stop(context.Background())
	serveErr := receive(t, done)

	require.NoError(t, err)
	require.NoError(t, serveErr)
	require.Error(t, receive(t, readDone))
	require.True(t, receive(t, closed))
	_, err = os.Lstat(c.path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestClientCancellationReachesSource(t *testing.T) {
	c := fixtureClient(t, "cancel")
	reading, canceled := make(chan struct{}), make(chan struct{})
	startFixture(t, c, liveBinding(c.server), func(context.Context) (Source, error) {
		return fixtureSource{
			read: func(ctx context.Context) (status.Snapshot, error) {
				close(reading)
				<-ctx.Done()
				close(canceled)
				return status.Snapshot{}, ctx.Err()
			},
			close: func() error { return nil },
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := c.ReadStatus(ctx)
		result <- err
	}()
	receive(t, reading)

	cancel()

	require.ErrorIs(t, receive(t, result), context.Canceled)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("client disconnect did not cancel source read")
	}

	_, err := c.Ping(context.Background())
	require.NoError(t, err)
}

func TestServerReplacementDuringReadRejectsSnapshot(t *testing.T) {
	c := fixtureClient(t, "server-old")
	var alive atomic.Bool
	alive.Store(true)
	reading, finish := make(chan struct{}), make(chan struct{})
	binding := Binding{
		Server: c.server,
		Alive:  func(context.Context) bool { return alive.Load() },
	}
	done := startFixture(t, c, binding, func(context.Context) (Source, error) {
		return fixtureSource{
			read: func(ctx context.Context) (status.Snapshot, error) {
				close(reading)
				select {
				case <-finish:
				case <-ctx.Done():
				}

				return status.Snapshot{}, nil
			},
			close: func() error { return nil },
		}, nil
	})

	result := make(chan error, 1)
	go func() {
		_, err := c.ReadStatus(context.Background())
		result <- err
	}()
	receive(t, reading)

	alive.Store(false)
	close(finish)

	require.Error(t, receive(t, result))
	require.NoError(t, receive(t, done))
}

func TestCanceledLifetimeCheckDoesNotStopDaemon(t *testing.T) {
	c := fixtureClient(t, "cancel-check")
	var block atomic.Bool
	checking, canceled := make(chan struct{}), make(chan struct{})
	binding := Binding{
		Server: c.server,
		Alive: func(ctx context.Context) bool {
			if block.Swap(false) {
				close(checking)
				<-ctx.Done()
				close(canceled)
				return false
			}

			return true
		},
	}
	startFixture(t, c, binding, staticSource(status.Snapshot{}))
	block.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := c.Ping(ctx)
		result <- err
	}()
	receive(t, checking)

	cancel()
	receive(t, canceled)

	require.ErrorIs(t, receive(t, result), context.Canceled)
	_, err := c.Ping(context.Background())
	require.NoError(t, err)
}

func TestConcurrentRequestsAreBoundedAndSlotsRecover(t *testing.T) {
	c := fixtureClient(t, "bounded")
	reading := make(chan struct{}, maxClients)
	startFixture(t, c, liveBinding(c.server), func(context.Context) (Source, error) {
		return fixtureSource{
			read: func(ctx context.Context) (status.Snapshot, error) {
				reading <- struct{}{}
				<-ctx.Done()
				return status.Snapshot{}, ctx.Err()
			},
			close: func() error { return nil },
		}, nil
	})
	var connections []net.Conn
	for range maxClients {
		conn, err := net.Dial("unix", c.path)
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = conn.Close()
		})
		connections = append(connections, conn)
		require.NoError(t, writeFrame(conn, maxRequestBytes, request{protocolVersion, c.server, "snapshot"}))
		receive(t, reading)
	}

	_, err := c.Ping(context.Background())
	for _, conn := range connections {
		_ = conn.Close()
	}

	require.Error(t, err)
	require.Eventually(t, func() bool {
		_, err := c.Ping(context.Background())
		return err == nil
	}, time.Second, time.Millisecond)
}

func TestIdleDaemonStopsWhenServerDies(t *testing.T) {
	c := fixtureClient(t, "dead-server")
	var alive atomic.Bool
	alive.Store(true)
	binding := Binding{
		Server: c.server,
		Alive:  func(context.Context) bool { return alive.Load() },
	}
	done := startFixture(t, c, binding, staticSource(status.Snapshot{}))

	alive.Store(false)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("daemon outlived server")
	}
}

func TestSingletonDoesNotStartSecondSource(t *testing.T) {
	c := fixtureClient(t, "singleton")
	startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	started := false

	err := Serve(context.Background(), liveBinding(c.server), func(context.Context) (Source, error) {
		started = true
		return nil, errors.New("unexpected second source")
	})

	require.NoError(t, err)
	require.False(t, started)
	_, err = c.Ping(context.Background())
	require.NoError(t, err)
}

func TestStartupFailureReleasesEndpointAndClosesSource(t *testing.T) {
	c := fixtureClient(t, "startup-failure")
	closed := false
	alive := true
	binding := Binding{
		Server: c.server,
		Alive:  func(context.Context) bool { return alive },
	}

	err := Serve(context.Background(), binding, func(context.Context) (Source, error) {
		alive = false
		return fixtureSource{
			close: func() error {
				closed = true
				return nil
			},
		}, nil
	})

	require.ErrorContains(t, err, "changed during startup")
	require.True(t, closed)
	release, err := lockEndpoint(c.path)
	require.NoError(t, err)
	require.NotNil(t, release)
	release()
}

func TestSourceFailureDoesNotBecomeEmptySuccess(t *testing.T) {
	c := fixtureClient(t, "read-failure")
	startFixture(t, c, liveBinding(c.server), func(context.Context) (Source, error) {
		return fixtureSource{
			read:  func(context.Context) (status.Snapshot, error) { return status.Snapshot{}, errors.New("fixture error") },
			close: func() error { return nil },
		}, nil
	})

	_, err := c.ReadStatus(context.Background())

	require.ErrorIs(t, err, ErrUnavailable)
}

func TestEndpointRecoveryRemovesStaleSocket(t *testing.T) {
	c := fixtureClient(t, "stale")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: c.path, Net: "unix"})
	require.NoError(t, err)
	listener.SetUnlinkOnClose(false)
	require.NoError(t, os.Chmod(c.path, 0600))
	require.NoError(t, listener.Close())

	err = removeStaleEndpoint(context.Background(), c.path)

	require.NoError(t, err)
	_, err = os.Lstat(c.path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestEndpointRecoveryPreservesNonSocketFiles(t *testing.T) {
	c := fixtureClient(t, "non-socket")
	require.NoError(t, os.WriteFile(c.path, []byte("keep"), 0600))

	err := removeStaleEndpoint(context.Background(), c.path)

	require.ErrorContains(t, err, "unsafe")
	data, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
}

func TestEndpointCleanupPreservesReplacement(t *testing.T) {
	c := fixtureClient(t, "replacement")
	listener, cleanup, err := listen(c.path)
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, listener.Close())
	require.NoError(t, os.Rename(c.path, c.path+".old"))
	require.NoError(t, os.WriteFile(c.path, []byte("replacement"), 0600))

	cleanup()

	data, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(data))
}
