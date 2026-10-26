package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const incompatibleEpoch = "fedcba9876543210fedcba9876543210"

// runIncompatibleFixture imitates a daemon of the previous protocol: it owns
// the endpoint lock, answers every request with a mismatch reply and exits
// cleanly on SIGTERM.
func runIncompatibleFixture(server string) int {
	terminated, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(terminated, 15*time.Second)
	defer cancel()

	path, err := endpoint(server)
	if err != nil || prepareEndpoint(path) != nil {
		return 1
	}

	lock, err := lockEndpoint(path)
	if err != nil || lock == nil {
		return 1
	}
	defer lock.Close()

	if err := lock.setOwner(incompatibleEpoch); err != nil {
		return 1
	}

	listener, cleanup, err := listen(path)
	if err != nil {
		return 1
	}
	defer cleanup()

	served := serveMismatch(listener, server, os.Getpid(), incompatibleEpoch)
	<-ctx.Done()
	_ = listener.Close()
	<-served

	if terminated.Err() == nil {
		return 1
	}
	return 0
}

// serveMismatch answers each connection as a daemon of the previous protocol
// until the listener closes. The returned channel closes when it stops.
func serveMismatch(listener *net.UnixListener, server string, pid int, epoch string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}

			_ = conn.SetDeadline(time.Now().Add(time.Second))
			if _, err := readFrameData(conn, maxRequestBytes); err == nil {
				row := reply{Version: protocolVersion - 1, Server: server, PID: pid, Epoch: epoch, Error: protocolMismatch}
				_ = writeFrame(conn, maxReplyBytes, row)
			}
			_ = conn.Close()
		}
	}()
	return done
}

func startIncompatibleFixture(t *testing.T, c *Client) <-chan error {
	t.Helper()

	executable, err := os.Executable()
	require.NoError(t, err)

	exited, err := startProcess(t.Context(), executable, []string{"__incompatible-daemon", c.server})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		observed, err := c.probe(t.Context())
		return err == nil && observed.state == Incompatible
	}, 3*time.Second, 10*time.Millisecond)
	return exited
}

// catchTermination keeps SIGTERM from ending the test binary. A broken identity
// check would signal this process; the test then fails on its assertions.
func catchTermination(t *testing.T) {
	t.Helper()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(signals) })
}

func TestEnsureReplacesDaemonOfAnotherBuild(t *testing.T) {
	c := fixtureClient(t, "replace-build")
	t.Cleanup(func() { stopProcessFixture(t, c) })
	executable, err := os.Executable()
	require.NoError(t, err)
	_, err = Ensure(t.Context(), c.server, "previous-build", executable, buildArguments(c.server, "previous-build"))
	require.NoError(t, err)
	previous, err := c.Ping(t.Context())
	require.NoError(t, err)

	_, err = Ensure(t.Context(), c.server, fixtureBuild, executable, fixtureArguments(c.server))
	current, pingErr := c.Ping(t.Context())
	data, logErr := os.ReadFile(c.LogPath())

	require.NoError(t, err)
	require.NoError(t, pingErr)
	require.NoError(t, logErr)
	require.Equal(t, "previous-build", previous.Build)
	require.Equal(t, fixtureBuild, current.Build)
	require.NotEqual(t, previous.Epoch, current.Epoch)
	require.Equal(t, 1, strings.Count(string(data), `event="stop_requested"`))
}

func TestEnsureReplacesIncompatibleDaemon(t *testing.T) {
	c := fixtureClient(t, "replace-protocol")
	exited := startIncompatibleFixture(t, c)
	t.Cleanup(func() { stopProcessFixture(t, c) })
	executable, err := os.Executable()
	require.NoError(t, err)

	_, err = Ensure(t.Context(), c.server, fixtureBuild, executable, fixtureArguments(c.server))
	current, pingErr := c.Ping(t.Context())

	require.NoError(t, err)
	require.NoError(t, receive(t, exited), "SIGTERM must end the incompatible daemon cleanly")
	require.NoError(t, pingErr)
	require.Equal(t, fixtureBuild, current.Build)
}

func TestStopTerminatesIncompatibleDaemon(t *testing.T) {
	c := fixtureClient(t, "stop-protocol")
	exited := startIncompatibleFixture(t, c)

	info, err := Stop(t.Context(), c.server)

	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, incompatibleEpoch, info.Epoch)
	require.NoError(t, receive(t, exited), "SIGTERM must end the incompatible daemon cleanly")
}

func TestStopRefusesUnconfirmedIncompatibleDaemon(t *testing.T) {
	catchTermination(t)

	bystander := exec.Command("sleep", "30")
	require.NoError(t, bystander.Start())
	t.Cleanup(func() {
		_ = bystander.Process.Kill()
		_ = bystander.Wait()
	})

	for _, tc := range []struct {
		name        string
		reportedPID int
		lockOwner   string
		holdLock    bool
	}{
		{"reported PID does not own the socket", bystander.Process.Pid, incompatibleEpoch, true},
		{"lock belongs to another instance", os.Getpid(), "0123456789abcdef0123456789abcdef", true},
		{"no process holds the lock", os.Getpid(), incompatibleEpoch, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, "unconfirmed")
			lock, err := lockEndpoint(c.path)
			require.NoError(t, err)
			require.NoError(t, lock.setOwner(tc.lockOwner))
			if tc.holdLock {
				t.Cleanup(func() { require.NoError(t, lock.Close()) })
			} else {
				require.NoError(t, lock.Close())
			}
			listener, cleanup, err := listen(c.path)
			require.NoError(t, err)
			served := serveMismatch(listener, c.server, tc.reportedPID, incompatibleEpoch)
			t.Cleanup(func() {
				_ = listener.Close()
				<-served
				cleanup()
			})

			_, err = Stop(t.Context(), c.server)

			require.ErrorIs(t, err, ErrIdentity)
		})
	}
}

func TestTerminateRefusesInstanceThatChangedAfterObservation(t *testing.T) {
	catchTermination(t)
	c := fixtureClient(t, "changed-incompatible")
	lock, err := lockEndpoint(c.path)
	require.NoError(t, err)
	require.NoError(t, lock.setOwner(incompatibleEpoch))
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	listener, cleanup, err := listen(c.path)
	require.NoError(t, err)
	served := serveMismatch(listener, c.server, os.Getpid(), incompatibleEpoch)
	t.Cleanup(func() {
		_ = listener.Close()
		<-served
		cleanup()
	})
	observed := Info{PID: os.Getpid(), Epoch: "0123456789abcdef0123456789abcdef"}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err = c.terminateIncompatible(ctx, observed)

	require.ErrorIs(t, err, ErrIdentity)
}

func TestEnsureRequiresBuildIdentity(t *testing.T) {
	c := fixtureClient(t, "missing-build")
	prepared := false

	_, err := Ensure(t.Context(), c.server, "", "", func(context.Context) ([]string, error) {
		prepared = true
		return nil, errors.New("unexpected launch preparation")
	})

	require.ErrorContains(t, err, "build identity")
	require.False(t, prepared)
}
