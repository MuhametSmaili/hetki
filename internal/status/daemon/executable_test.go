package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fakeExecutable(t *testing.T) *executableImage {
	t.Helper()

	path := filepath.Join(t.TempDir(), "hetki")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0700))
	running, err := os.Stat(path)
	require.NoError(t, err)

	return &executableImage{path: path, running: running}
}

func TestExecutableImageDetectsReplacement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, path string)
		want   bool
	}{
		{"unchanged", func(*testing.T, string) {}, false},
		{"replaced by rename", func(t *testing.T, path string) {
			next := path + ".next"
			require.NoError(t, os.WriteFile(next, []byte("new"), 0700))
			require.NoError(t, os.Rename(next, path))
		}, true},
		{"touched", func(t *testing.T, path string) {
			later := time.Now().Add(time.Hour)
			require.NoError(t, os.Chtimes(path, later, later))
		}, false},
		{"removed", func(t *testing.T, path string) {
			require.NoError(t, os.Remove(path))
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := fakeExecutable(t)
			tc.change(t, image.path)

			replaced, err := image.replaced()

			require.NoError(t, err)
			require.Equal(t, tc.want, replaced)
		})
	}
}

func TestDaemonStopsWhenItsExecutableIsReplaced(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "daemon.log")
	logs, err := openDaemonLog(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = logs.close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	image := fakeExecutable(t)
	d := &daemon{binding: liveBinding("server"), cancel: cancel, logs: logs, executable: image}
	require.NoError(t, os.Remove(image.path))
	done := make(chan struct{})

	go func() {
		defer close(done)
		d.watchLifetime(ctx)
	}()
	receive(t, done)

	require.ErrorIs(t, ctx.Err(), context.Canceled)
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(data), `event="executable_replaced"`)
}
