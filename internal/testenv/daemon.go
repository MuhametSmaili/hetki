// Package testenv contains test-only environment and daemon teardown helpers.
package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// PrivateHome isolates runtime paths, including the daemon's short-socket
// fallback. Build test executables before calling it to retain normal Go caches.
// Failed tests retain their directory because teardown may be incomplete.
func PrivateHome(t testing.TB) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "ht-")
	if err != nil {
		t.Fatalf("create private test home: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("retained private test home: %s", dir)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove private test home: %v", err)
		}
	})

	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "TMPDIR"} {
		t.Setenv(key, dir)
	}
	return dir
}

// WaitUnlocked waits for the daemon's final ownership boundary: its lock remains
// held through worker/source shutdown and log closure. It never creates or
// unlinks the lock. Call only after launch attempts finish; an absent lock says
// nothing about a child that has not started yet.
func WaitUnlocked(ctx context.Context, path string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open daemon fixture lock %s: %w", path, err)
	}
	defer func() {
		err = errors.Join(err, unix.Close(fd))
	}()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return fmt.Errorf("lock daemon fixture %s: %w", path, err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for daemon fixture lock %s: %w", path, ctx.Err())
		case <-ticker.C:
		}
	}
}
