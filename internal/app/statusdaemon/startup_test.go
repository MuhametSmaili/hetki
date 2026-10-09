package statusdaemon

import (
	"context"
	"os"
	"os/signal"
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
		err := Serve(ctx, liveBinding(os.Args[2]), staticSource(status.Snapshot{}))
		cancel()
		stop()

		if err != nil {
			os.Exit(1)
		}

		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestConcurrentLaunchAndCrashReconnect(t *testing.T) {
	c := fixtureClient(t, "process-fixture")
	executable, err := os.Executable()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = c.Stop(context.Background())
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

	for i := range infos {
		require.NoError(t, errs[i])
		require.Equal(t, infos[0], infos[i])
	}

	process, err := os.FindProcess(infos[0].PID)
	require.NoError(t, err)
	require.NoError(t, process.Kill())
	_ = process.Release()
	require.Eventually(t, func() bool {
		_, err := c.Ping(context.Background())
		return isMissingEndpoint(err)
	}, 3*time.Second, 10*time.Millisecond)

	restarted, err := Ensure(context.Background(), c.server, executable)

	require.NoError(t, err)
	info, err := restarted.Ping(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, infos[0].Epoch, info.Epoch)

	snapshot, err := restarted.ReadStatus(context.Background())
	require.NoError(t, err)
	require.Zero(t, snapshot.Len())

	require.NoError(t, restarted.Stop(context.Background()))
	require.Eventually(t, func() bool {
		_, err := os.Lstat(c.path)
		return os.IsNotExist(err)
	}, 3*time.Second, 10*time.Millisecond)
}
