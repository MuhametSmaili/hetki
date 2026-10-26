package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	if len(os.Args) == 3 && os.Args[1] == "__incompatible-daemon" {
		os.Exit(runIncompatibleFixture(os.Args[2]))
	}

	if len(os.Args) >= 3 && os.Args[1] == "__status-daemon" {
		build := fixtureBuild
		if len(os.Args) == 4 {
			build = os.Args[3]
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := Serve(ctx, buildBinding(os.Args[2], build), processFixtureSource(os.Args[2]))
		cancel()
		stop()

		if err != nil {
			os.Exit(1)
		}

		os.Exit(0)
	}

	os.Exit(m.Run())
}

func fixtureArguments(server string) func(context.Context) ([]string, error) {
	return buildArguments(server, fixtureBuild)
}

func buildArguments(server, build string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) {
		return []string{"__status-daemon", server, build}, nil
	}
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

	prepared := false
	client, err := Ensure(t.Context(), c.server, fixtureBuild, executable, func(context.Context) ([]string, error) {
		prepared = true
		return nil, errors.New("unexpected launch preparation")
	})
	require.NoError(t, err)
	require.False(t, prepared, "reuse must not prepare another launch")
	second, err := client.Ping(t.Context())
	data, readErr := os.ReadFile(c.LogPath())

	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NoError(t, readErr)
	require.Contains(t, string(data), "epoch="+first.Epoch)
	require.Equal(t, 1, strings.Count(string(data), `event="starting"`))
}

func TestEnsureRetriesSettledLaunchWithoutRepeatingPreparation(t *testing.T) {
	client := fixtureClient(t, "settled-launch")
	t.Cleanup(func() { stopProcessFixture(t, client) })
	executable, err := exec.LookPath("sh")
	require.NoError(t, err)
	fixture, err := os.Executable()
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "first-attempt")
	prepared := 0

	_, err = Ensure(t.Context(), client.server, fixtureBuild, executable, func(context.Context) ([]string, error) {
		prepared++
		return []string{"-c", `if [ ! -f "$1" ]; then : > "$1"; exit 0; fi; exec "$2" __status-daemon "$3"`, "fixture", marker, fixture, client.server}, nil
	})

	require.NoError(t, err)
	require.Equal(t, 1, prepared)
	_, err = client.Ping(t.Context())
	require.NoError(t, err)
}

func TestStartupWaitsForPrivateSocketPermissions(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(context.Context, *Client) error
	}{
		{"ensure", func(ctx context.Context, client *Client) error {
			_, err := Ensure(ctx, client.server, fixtureBuild, "", func(context.Context) ([]string, error) {
				return nil, errors.New("unexpected launch preparation")
			})
			return err
		}},
		{"readiness", func(ctx context.Context, client *Client) error {
			return client.waitReady(ctx, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, "socket-permissions")
			startFixture(t, client, liveBinding(client.server), staticSource(status.Snapshot{}))
			before, err := client.Ping(t.Context())
			require.NoError(t, err)
			require.NoError(t, os.Chmod(client.path, 0700))
			t.Cleanup(func() { require.NoError(t, os.Chmod(client.path, 0600)) })
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()

			err = tc.run(ctx, client)

			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.ErrorIs(t, err, errEndpointPermissions)
			require.NoError(t, os.Chmod(client.path, 0600))
			require.NoError(t, tc.run(t.Context(), client))
			after, err := client.Ping(t.Context())
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestEnsurePreservesPreparationFailureWithoutLaunching(t *testing.T) {
	c := fixtureClient(t, "preparation-failure")
	failure := errors.New("fixture preparation failed")
	preparations := 0

	_, err := Ensure(t.Context(), c.server, fixtureBuild, filepath.Join(t.TempDir(), "missing"), func(context.Context) ([]string, error) {
		preparations++
		return nil, failure
	})
	_, logErr := os.Lstat(c.LogPath())
	_, socketErr := os.Lstat(c.path)

	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, preparations)
	require.ErrorIs(t, logErr, os.ErrNotExist)
	require.ErrorIs(t, socketErr, os.ErrNotExist)
}

func TestEnsureLaunchUsesCompleteCompositionArguments(t *testing.T) {
	c := fixtureClient(t, "composition-arguments")
	executable, err := exec.LookPath("sh")
	require.NoError(t, err)

	_, err = Ensure(t.Context(), c.server, fixtureBuild, executable, func(context.Context) ([]string, error) {
		return []string{"-c", "exit 7"}, nil
	})

	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode(), "launcher must not prepend its own command or server arguments")
}

func TestEnsureCancellationPreventsLaunch(t *testing.T) {
	for _, beforeProbe := range []bool{true, false} {
		name := "during preparation"
		if beforeProbe {
			name = "before probe"
		}
		t.Run(name, func(t *testing.T) {
			c := fixtureClient(t, "canceled-launch")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if beforeProbe {
				cancel()
			}
			prepared := false

			_, err := Ensure(ctx, c.server, fixtureBuild, filepath.Join(t.TempDir(), "missing"), func(prepareCtx context.Context) ([]string, error) {
				prepared = true
				cancel()
				require.ErrorIs(t, prepareCtx.Err(), context.Canceled)
				return []string{"unused"}, nil
			})

			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, !beforeProbe, prepared)
		})
	}
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
		exited, err := startProcess(t.Context(), executable, []string{"__status-daemon", c.server})
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
	_, err = Ensure(context.Background(), c.server, fixtureBuild, executable, fixtureArguments(c.server))
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

	_, restartErr := Ensure(context.Background(), c.server, fixtureBuild, executable, fixtureArguments(c.server))
	info, pingErr := c.Ping(context.Background())
	snapshot, readErr := readFirst(context.Background(), c)
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
