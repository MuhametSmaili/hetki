package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func changeServiceDirectory(t *testing.T, dir, change string) func() {
	t.Helper()

	if change == "permissions" {
		require.NoError(t, os.Chmod(dir, 0777))
		restore := sync.OnceFunc(func() { require.NoError(t, os.Chmod(dir, 0700)) })
		t.Cleanup(restore)
		return restore
	}

	original := dir + "-original"
	require.NoError(t, os.Rename(dir, original))
	restore := sync.OnceFunc(func() {
		if err := os.Remove(dir); !errors.Is(err, os.ErrNotExist) {
			require.NoError(t, err)
		}
		require.NoError(t, os.Rename(original, dir))
	})
	t.Cleanup(restore)
	require.NoError(t, os.Symlink(original, dir))
	return restore
}

func TestCachedStopRejectsChangedDirectoryBeforeStopping(t *testing.T) {
	for _, change := range []string{"permissions", "symlink"} {
		t.Run(change, func(t *testing.T) {
			client := fixtureClient(t, "changed-stop-directory")
			startFixture(t, client, liveBinding(client.server), staticSource(status.Snapshot{}))
			before, err := client.Ping(t.Context())
			require.NoError(t, err)
			restore := changeServiceDirectory(t, filepath.Dir(client.path), change)

			err = client.Stop(t.Context())

			require.Error(t, err)
			restore()
			after, err := client.Ping(t.Context())
			require.NoError(t, err)
			require.Equal(t, before, after, "an unsafe directory must prevent the stop request")
		})
	}
}

func TestStopRejectsDirectoryChangesDuringCleanup(t *testing.T) {
	for _, change := range []string{"permissions", "symlink"} {
		t.Run(change, func(t *testing.T) {
			client, closing, release, done := gatedCleanup(t, "changed-cleanup-directory")
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			stopped := make(chan error, 1)
			go func() {
				defer close(stopped)
				_, err := Stop(ctx, client.server)
				stopped <- err
			}()
			defer func() {
				cancel()
				_ = receive(t, stopped)
			}()
			receive(t, closing)
			restore := changeServiceDirectory(t, filepath.Dir(client.path), change)

			release()
			err := receive(t, stopped)

			require.Error(t, err, "changed directory safety must prevent successful cleanup confirmation")
			restore()
			require.NoError(t, receive(t, done), "the accepted stop must still finish")
		})
	}
}
