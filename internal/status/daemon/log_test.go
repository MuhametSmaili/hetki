package daemon

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func TestLifecycleLogSurvivesDaemonShutdown(t *testing.T) {
	c := fixtureClient(t, "lifecycle-log")
	done := startFixture(t, c, liveBinding(c.server), staticSource(status.Snapshot{}))
	info, err := c.Ping(context.Background())
	require.NoError(t, err)

	stopErr := c.Stop(context.Background())
	serveErr := receive(t, done)
	data, readErr := os.ReadFile(c.LogPath())
	stat, statErr := os.Stat(c.LogPath())

	require.NoError(t, stopErr)
	require.NoError(t, serveErr)
	require.NoError(t, readErr)
	require.NoError(t, statErr)
	require.Contains(t, string(data), `event="starting"`)
	require.Contains(t, string(data), "epoch="+info.Epoch)
	require.Contains(t, string(data), `event="stop_requested"`)
	require.Contains(t, string(data), `event="stopped"`)
	require.Equal(t, os.FileMode(0600), stat.Mode().Perm())
}

func TestRepeatedSourceFailuresProduceOneSafeBoundedRecord(t *testing.T) {
	c := fixtureClient(t, "source-error-log")
	failure := errors.New("unavailable\n\x1b[31m" + strings.Repeat("x", 1<<20))
	startFixture(t, c, liveBinding(c.server), func(context.Context, func(error)) (Source, error) {
		return fixtureSource{
			read: func(context.Context) (status.Snapshot, error) {
				return status.Snapshot{}, failure
			},
			close: func() error { return nil },
		}, nil
	})
	errs := make([]error, 8)
	var workers sync.WaitGroup

	for i := range errs {
		workers.Go(func() {
			_, errs[i] = readFirst(context.Background(), c)
		})
	}
	workers.Wait()
	data, readErr := os.ReadFile(c.LogPath())
	text := string(data)

	for _, err := range errs {
		require.ErrorIs(t, err, ErrUnavailable)
	}
	require.NoError(t, readErr)
	require.Equal(t, 1, strings.Count(text, `event="source_read_failed"`))
	require.Contains(t, text, `unavailable\n\x1b[31m`)
	require.Contains(t, text, "(truncated)")
	require.NotContains(t, text, "\x1b")
	require.Less(t, len(data), 8192)
}

func TestAcquisitionDiagnosticsUseDaemonLogWithoutSnapshotRequests(t *testing.T) {
	c := fixtureClient(t, "acquisition-log")
	done := startFixture(t, c, liveBinding(c.server), func(_ context.Context, report func(error)) (Source, error) {
		for range 10 {
			report(errors.New("native acquisition unavailable"))
		}
		report(nil)

		return fixtureSource{
			read:  func(context.Context) (status.Snapshot, error) { return status.Snapshot{}, nil },
			close: func() error { return nil },
		}, nil
	})

	require.NoError(t, c.Stop(t.Context()))
	require.NoError(t, receive(t, done))
	data, err := os.ReadFile(c.LogPath())

	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), `event="source_failed"`))
	require.Contains(t, string(data), "native acquisition unavailable")
}

func TestLogRotationKeepsOnlyTwoBoundedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	writer := &logFile{path: path}
	require.NoError(t, writer.open())
	t.Cleanup(func() {
		if writer.file != nil {
			_ = writer.file.Close()
		}
	})
	first := bytes.Repeat([]byte("a"), maxLogBytes)
	second := bytes.Repeat([]byte("b"), maxLogBytes)
	third := bytes.Repeat([]byte("c"), maxLogBytes)
	_, err := writer.Write(first)
	require.NoError(t, err)
	_, err = writer.Write(second)
	require.NoError(t, err)

	_, writeErr := writer.Write(third)
	current, currentErr := os.ReadFile(path)
	previous, previousErr := os.ReadFile(path + ".1")
	files, directoryErr := os.ReadDir(filepath.Dir(path))

	require.NoError(t, writeErr)
	require.NoError(t, currentErr)
	require.NoError(t, previousErr)
	require.NoError(t, directoryErr)
	require.Equal(t, third, current)
	require.Equal(t, second, previous)
	require.Len(t, files, 2)
}

func TestLogRejectsSymlinkWithoutChangingTarget(t *testing.T) {
	c := fixtureClient(t, "symlink-log")
	target := filepath.Join(t.TempDir(), "keep")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0600))
	require.NoError(t, os.Symlink(target, c.LogPath()))

	err := prepareLog(c.LogPath())
	data, readErr := os.ReadFile(target)

	require.Error(t, err)
	require.NoError(t, readErr)
	require.Equal(t, "keep", string(data))
}

func TestFailedStartupReturnsExitErrorAndLeavesDiagnostic(t *testing.T) {
	c := fixtureClient(t, "failed-source")
	executable, err := os.Executable()
	require.NoError(t, err)

	_, err = Ensure(context.Background(), c.server, fixtureBuild, executable, fixtureArguments(c.server))
	data, readErr := os.ReadFile(c.LogPath())
	_, endpointErr := os.Lstat(c.path)

	require.ErrorContains(t, err, "exited before readiness")
	require.ErrorContains(t, err, c.LogPath())
	require.NoError(t, readErr)
	require.Contains(t, string(data), "fixture could not initialize its source")
	require.ErrorIs(t, endpointErr, os.ErrNotExist)
}

func TestSourcePanicLeavesDiagnosticWithoutBeingSwallowed(t *testing.T) {
	c := fixtureClient(t, "panic-source")
	executable, err := os.Executable()
	require.NoError(t, err)
	_, err = Ensure(context.Background(), c.server, fixtureBuild, executable, fixtureArguments(c.server))
	require.NoError(t, err)
	t.Cleanup(func() {
		stopProcessFixture(t, c)
	})

	_, err = readFirst(context.Background(), c)

	require.Error(t, err)
	require.Eventually(t, func() bool {
		_, err := c.Ping(context.Background())
		return isMissingEndpoint(err)
	}, 3*time.Second, 10*time.Millisecond)
	data, readErr := os.ReadFile(c.LogPath())
	require.NoError(t, readErr)
	require.Contains(t, string(data), `event="panic"`)
	require.Contains(t, string(data), "fixture source panic")
	require.Contains(t, string(data), "stack=")
}
