package daemon

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "__status-daemon" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := Serve(ctx, liveBinding(os.Args[2]), processFixtureSource(os.Args[2]))
		cancel()
		stop()

		if err != nil {
			os.Exit(1)
		}

		os.Exit(0)
	}

	os.Exit(m.Run())
}

func processFixtureSource(server string) SourceFactory {
	if strings.HasPrefix(server, "failed-source-") {
		return func(context.Context, func(error)) (Source, error) {
			return nil, errors.New("fixture could not initialize its source")
		}
	}

	if strings.HasPrefix(server, "panic-source-") {
		return func(context.Context, func(error)) (Source, error) {
			return fixtureSource{
				read: func(context.Context) (status.Snapshot, error) {
					panic("fixture source panic")
				},
				close: func() error { return nil },
			}, nil
		}
	}

	return staticSource(status.Snapshot{})
}

func TestConcurrentLaunchUsesSingleDaemon(t *testing.T) {
	c := fixtureClient(t, "concurrent-start")
	executable, err := os.Executable()
	require.NoError(t, err)
	exits := launchProcessFixtures(t, c, executable, 4)
	require.NoError(t, c.waitReady(t.Context(), nil))
	for range 3 {
		require.NoError(t, receive(t, exits), "losing starters must exit while the winner holds its lock")
	}

	first, err := c.Ping(t.Context())
	require.NoError(t, err)

	client, err := Ensure(t.Context(), c.server, executable)
	require.NoError(t, err)
	second, err := client.Ping(t.Context())
	data, readErr := os.ReadFile(c.LogPath())

	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NoError(t, readErr)
	require.Contains(t, string(data), "epoch="+first.Epoch)
	require.Equal(t, 1, strings.Count(string(data), `event="starting"`))
}

// Retain each real launch's completion signal; Ensure may return another child's
// readiness before its own child has tried the lock.
func launchProcessFixtures(t *testing.T, c *Client, executable string, count int) <-chan error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	exits := make(chan error, count)
	var waiters sync.WaitGroup
	t.Cleanup(func() {
		defer cancel()
		stopProcessFixture(t, c)
		waiters.Wait()
		for len(exits) > 0 {
			if err := <-exits; err != nil {
				t.Errorf("daemon child did not exit cleanly: %v", err)
			}
		}
	})

	for range count {
		exited, err := startProcess(t.Context(), c.server, executable, nil)
		require.NoError(t, err)
		waiters.Go(func() {
			select {
			case err := <-exited:
				exits <- err
			case <-ctx.Done():
				exits <- ctx.Err()
			}
		})
	}
	return exits
}

func TestCrashReconnectStartsFreshDaemon(t *testing.T) {
	c := fixtureClient(t, "crash-reconnect")
	executable, err := os.Executable()
	require.NoError(t, err)
	t.Cleanup(func() {
		stopProcessFixture(t, c)
	})
	_, err = Ensure(context.Background(), c.server, executable)
	require.NoError(t, err)
	previous, err := c.Ping(context.Background())
	require.NoError(t, err)
	process, err := os.FindProcess(previous.PID)
	require.NoError(t, err)
	require.NoError(t, process.Kill())
	_ = process.Release()
	require.Eventually(t, func() bool {
		_, err := c.Ping(context.Background())
		return isMissingEndpoint(err)
	}, 3*time.Second, 10*time.Millisecond)

	_, restartErr := Ensure(context.Background(), c.server, executable)
	info, pingErr := c.Ping(context.Background())
	snapshot, readErr := c.ReadStatus(context.Background())
	data, logErr := os.ReadFile(c.LogPath())

	require.NoError(t, restartErr)
	require.NoError(t, pingErr)
	require.NoError(t, readErr)
	require.NoError(t, logErr)
	require.NotEqual(t, previous.Epoch, info.Epoch)
	require.Zero(t, snapshot.Len())
	require.Contains(t, string(data), "epoch="+previous.Epoch)
	require.Contains(t, string(data), "epoch="+info.Epoch)
	require.Equal(t, 2, strings.Count(string(data), `event="starting"`))
}

func stopProcessFixture(t *testing.T, c *Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Stop(ctx); err != nil && !isMissingEndpoint(err) {
		t.Errorf("stop daemon fixture: %v", err)
	}
	if err := testenv.WaitUnlocked(ctx, c.path+".lock"); err != nil {
		t.Errorf("daemon fixture teardown: %v", err)
	}
}
