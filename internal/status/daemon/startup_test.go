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

	"github.com/MSmaili/hetki/internal/status"
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
	t.Cleanup(func() {
		stopProcessFixture(t, c)
	})

	var workers sync.WaitGroup
	infos := make([]Info, 4)
	errs := make([]error, len(infos))

	for i := range infos {
		workers.Go(func() {
			var client *Client
			client, errs[i] = Ensure(context.Background(), c.server, executable)
			if errs[i] == nil {
				infos[i], errs[i] = client.Ping(context.Background())
			}
		})
	}
	workers.Wait()
	data, readErr := os.ReadFile(c.LogPath())

	for i := range infos {
		require.NoError(t, errs[i])
		require.Equal(t, infos[0], infos[i])
	}
	require.NoError(t, readErr)
	require.Contains(t, string(data), "epoch="+infos[0].Epoch)
	require.Equal(t, 1, strings.Count(string(data), `event="starting"`))
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

	err := c.Stop(context.Background())
	if isMissingEndpoint(err) {
		return
	}
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, err := os.Lstat(c.path)
		return os.IsNotExist(err)
	}, 3*time.Second, 10*time.Millisecond)
}
