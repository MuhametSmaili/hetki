package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
)

type fixtureSource struct {
	read    func(context.Context) (status.Snapshot, error)
	close   func() error
	changes <-chan struct{}
}

func (s fixtureSource) Changes() <-chan struct{} { return s.changes }

func (s fixtureSource) ReadView(ctx context.Context) (View, error) {
	values, err := s.read(ctx)
	return View{Values: values, Lease: time.Now().Add(time.Hour)}, err
}

func (s fixtureSource) Close() error {
	return s.close()
}

func fixtureClient(t testing.TB, server string) *Client {
	t.Helper()

	dir := testenv.PrivateHome(t)
	c, err := Connect(server + "-" + filepath.Base(dir))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := testenv.WaitUnlocked(ctx, c.path+".lock"); err != nil {
			t.Errorf("daemon fixture teardown: %v", err)
		}
	})

	return c
}

func TestFixtureContainsDaemonFiles(t *testing.T) {
	for _, longHome := range []bool{false, true} {
		name := "ordinary paths"
		if longHome {
			name = "long paths use private fallback"
		}
		t.Run(name, func(t *testing.T) {
			c := fixtureClient(t, "private-paths")
			root := os.Getenv("HOME")
			if longHome {
				long := filepath.Join(root, strings.Repeat("x", 120))
				t.Setenv("HOME", long)
				t.Setenv("XDG_CACHE_HOME", long)
				var err error
				c, err = Connect(c.server)
				require.NoError(t, err)
			}

			for _, path := range []string{c.path, c.LogPath()} {
				relative, err := filepath.Rel(root, path)

				require.NoError(t, err)
				require.True(t, filepath.IsLocal(relative), "daemon file escaped fixture: %s", path)
			}
			require.LessOrEqual(t, len(c.path), 100)
		})
	}
}

func TestClientAndServerRejectUnsafeDirectory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*testing.T, string)
	}{
		{"public permissions", func(t *testing.T, path string) {
			require.NoError(t, os.Chmod(path, 0755))
		}},
		{"symlink", func(t *testing.T, path string) {
			require.NoError(t, os.Rename(path, path+".original"))
			require.NoError(t, os.Symlink(path+".original", path))
		}},
		{"regular file", func(t *testing.T, path string) {
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testenv.PrivateHome(t)
			c, err := Connect("unsafe-directory")
			require.NoError(t, err)
			dir := filepath.Dir(c.path)
			tc.alter(t, dir)
			before, err := os.Lstat(dir)
			require.NoError(t, err)
			started := false

			_, connectErr := Connect(c.server)
			_, inspectErr := Inspect(t.Context(), c.server)
			serveErr := Serve(t.Context(), liveBinding(c.server), func(context.Context, func(error)) (Source, error) {
				started = true
				return nil, errors.New("unexpected source startup")
			})
			after, statErr := os.Lstat(dir)

			require.Error(t, connectErr)
			require.Error(t, inspectErr)
			require.Error(t, serveErr)
			require.False(t, started)
			require.NoError(t, statErr)
			require.True(t, os.SameFile(before, after))
			require.Equal(t, before.Mode(), after.Mode())
		})
	}
}

func startFixture(t testing.TB, c *Client, binding Binding, start SourceFactory) <-chan error {
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

func staticSource(snapshot status.Snapshot) SourceFactory {
	return func(context.Context, func(error)) (Source, error) {
		return fixtureSource{
			read:  func(context.Context) (status.Snapshot, error) { return snapshot, nil },
			close: func() error { return nil },
		}, nil
	}
}

// fixtureBuild is the build identity of daemons and clients in these tests.
const fixtureBuild = "fixture-build"

func liveBinding(server string) Binding {
	return buildBinding(server, fixtureBuild)
}

func buildBinding(server, build string) Binding {
	return Binding{Server: server, Build: build, Alive: func(context.Context) bool { return true }}
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

	got, err := readFirst(context.Background(), c)

	require.NoError(t, err)
	for pane, value := range values {
		require.Equal(t, value, got.Lookup(pane))
	}
	require.Equal(t, status.Unreported, got.At(status.PaneRef{Server: c.server, ID: "missing", Generation: "birth"}, deadline))
	require.Equal(t, status.Unknown, got.At(status.PaneRef{Server: c.server, ID: "working", Generation: "birth"}, deadline))
}

func TestShutdownCancelsReadsBeforeClosingSource(t *testing.T) {
	c := fixtureClient(t, "shutdown")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	reading := make(chan struct{})
	canceled := make(chan struct{})
	closed := make(chan bool, 1)
	finishClose := make(chan struct{})
	releaseClose := sync.OnceFunc(func() { close(finishClose) })
	defer releaseClose()
	done := startFixture(t, c, liveBinding(c.server), func(ctx context.Context, _ func(error)) (Source, error) {
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
				<-finishClose

				return nil
			},
		}, nil
	})

	readDone := make(chan error, 1)
	go func() {
		_, err := readFirst(context.Background(), c)
		readDone <- err
	}()
	receive(t, reading)

	stopDone := make(chan error, 1)
	go func() { stopDone <- c.Stop(ctx) }()

	require.True(t, receive(t, closed))
	require.Error(t, receive(t, readDone))
	_, err := os.Lstat(c.path)
	require.ErrorIs(t, err, os.ErrNotExist)
	waitCtx, cancelWait := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancelWait()
	require.ErrorIs(t, testenv.WaitUnlocked(waitCtx, c.path+".lock"), context.DeadlineExceeded)

	releaseClose()
	require.NoError(t, receive(t, stopDone))
	require.NoError(t, receive(t, done))
	require.NoError(t, testenv.WaitUnlocked(ctx, c.path+".lock"))
}

func TestServerReplacementDuringReadRejectsSnapshot(t *testing.T) {
	c := fixtureClient(t, "server-old")
	var alive atomic.Bool
	alive.Store(true)
	reading, finish := make(chan struct{}), make(chan struct{})
	binding := Binding{
		Server: c.server,
		Build:  fixtureBuild,
		Alive:  func(context.Context) bool { return alive.Load() },
	}
	done := startFixture(t, c, binding, func(context.Context, func(error)) (Source, error) {
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
		_, err := readFirst(context.Background(), c)
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
		Build:  fixtureBuild,
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

func dialIdle(t *testing.T, c *Client, count int) []net.Conn {
	t.Helper()
	connections := make([]net.Conn, 0, count)
	for range count {
		conn, err := net.Dial("unix", c.path)
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = conn.Close()
		})
		connections = append(connections, conn)
	}
	return connections
}

func TestRequestsBeyondTheClientLimitGetABusyReply(t *testing.T) {
	c := fixtureClient(t, "busy")
	startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	before, err := c.Ping(t.Context())
	require.NoError(t, err)
	dialIdle(t, c, maxClients)
	require.Eventually(t, func() bool {
		_, err := c.Ping(t.Context())
		return errors.Is(err, ErrBusy)
	}, time.Second, time.Millisecond)

	busy, pingErr := c.Ping(t.Context())
	inspection, inspectErr := Inspect(t.Context(), c.server)
	_, ensureErr := Ensure(t.Context(), c.server, fixtureBuild, "", func(context.Context) ([]string, error) {
		return nil, errors.New("a busy daemon of this build must be reused")
	})

	require.ErrorIs(t, pingErr, ErrBusy)
	require.Equal(t, before, busy, "a busy reply identifies the running instance and its build")
	require.NoError(t, inspectErr)
	require.Equal(t, Running, inspection.State)
	require.Equal(t, before, *inspection.Instance)
	require.NoError(t, ensureErr)
}

func TestConcurrentRequestsAreBoundedAndSlotsRecover(t *testing.T) {
	c := fixtureClient(t, "bounded")
	startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	connections := dialIdle(t, c, maxClients+maxBusyReplies)
	require.Eventually(t, func() bool {
		_, err := c.Ping(context.Background())
		return err != nil && !errors.Is(err, ErrBusy)
	}, time.Second, time.Millisecond, "beyond the busy replies, connections are closed")

	for _, conn := range connections {
		_ = conn.Close()
	}

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
		Build:  fixtureBuild,
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

	err := Serve(context.Background(), liveBinding(c.server), func(context.Context, func(error)) (Source, error) {
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
		Build:  fixtureBuild,
		Alive:  func(context.Context) bool { return alive },
	}

	err := Serve(context.Background(), binding, func(context.Context, func(error)) (Source, error) {
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
	lock, err := lockEndpoint(c.path)
	require.NoError(t, err)
	require.NotNil(t, lock)
	require.NoError(t, lock.Close())
}

func TestSourceFailureDoesNotBecomeEmptySuccess(t *testing.T) {
	c := fixtureClient(t, "read-failure")
	startFixture(t, c, liveBinding(c.server), func(context.Context, func(error)) (Source, error) {
		return fixtureSource{
			read:  func(context.Context) (status.Snapshot, error) { return status.Snapshot{}, errors.New("fixture error") },
			close: func() error { return nil },
		}, nil
	})

	_, err := readFirst(context.Background(), c)

	require.ErrorIs(t, err, ErrUnavailable)
}

func TestEndpointRecoveryRemovesStaleSocket(t *testing.T) {
	c := fixtureClient(t, "stale")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: c.path, Net: "unix"})
	require.NoError(t, err)
	listener.SetUnlinkOnClose(false)
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
