package daemon

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const staleConnectTimeout = 100 * time.Millisecond

// endpoint describes the location without reading or creating filesystem entries.
func endpoint(server string) (string, error) {
	if !validIdentity(server) {
		return "", fmt.Errorf("invalid status server identity")
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(server))
	name := fmt.Sprintf("%x.sock", sum[:16])
	dir := filepath.Join(cache, "hetki", "status")

	if len(filepath.Join(dir, name)) > 100 {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("hstatus-%d", os.Getuid()))
		name = fmt.Sprintf("%x.sock", sum[:8])
	}

	if len(filepath.Join(dir, name)) > 100 {
		return "", fmt.Errorf("status socket path exceeds native limit")
	}

	return filepath.Join(dir, name), nil
}

func prepareEndpoint(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return validateEndpointDirectory(path)
}

func validateEndpointDirectory(path string) error {
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}

	if !info.IsDir() || !ownedMode(info, 0700) {
		return fmt.Errorf("status directory must be user-owned and mode 0700")
	}

	return nil
}

func ownedMode(info os.FileInfo, mode os.FileMode) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid() && info.Mode().Perm() == mode
}

// A nil release means another process owns the endpoint. Keep the lock file
// after release so competing starters always lock the same inode.
func lockEndpoint(path string) (release func(), err error) {
	fd, err := unix.Open(path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if release == nil {
			_ = unix.Close(fd)
		}
	}()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}

	if int(stat.Uid) != os.Getuid() ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0777 != 0600 ||
		stat.Nlink != 1 {
		return nil, fmt.Errorf("unsafe status daemon lock")
	}

	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, nil
		}

		return nil, err
	}

	return func() {
		_ = unix.Close(fd)
	}, nil
}

func removeStaleEndpoint(ctx context.Context, path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSocket == 0 || !ownedMode(info, 0600) {
		return fmt.Errorf("unsafe status daemon endpoint")
	}

	conn, err := (&net.Dialer{Timeout: staleConnectTimeout}).DialContext(ctx, "unix", path)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("status endpoint is active without its lock")
	}

	if !isMissingEndpoint(err) {
		return err
	}

	return os.Remove(path)
}

func isMissingEndpoint(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
}

func listen(path string) (*net.UnixListener, func(), error) {
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, nil, err
	}
	listener.SetUnlinkOnClose(false)

	info, err := os.Lstat(path)
	cleanup := func() {
		_ = listener.Close()

		if current, err := os.Lstat(path); err == nil && info != nil && os.SameFile(info, current) {
			_ = os.Remove(path)
		}
	}

	if err == nil {
		err = os.Chmod(path, 0600)
	}

	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return listener, cleanup, nil
}
