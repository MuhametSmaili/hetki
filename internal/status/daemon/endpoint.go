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
)

const staleConnectTimeout = 100 * time.Millisecond

var errUnsafeDirectory = errors.New("status directory must be user-owned and mode 0700")

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
		return errUnsafeDirectory
	}

	return nil
}

func ownedMode(info os.FileInfo, mode os.FileMode) bool {
	return ownedByCurrentUser(info) && info.Mode().Perm() == mode
}

// ownedSocket checks the endpoint itself. Its mode is not checked: the private
// directory is the access boundary, because socket file permissions are not
// portable (unix(7)) and Go cannot set them before the socket is reachable.
func ownedSocket(info os.FileInfo) bool {
	return info.Mode()&os.ModeSocket != 0 && ownedByCurrentUser(info)
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

func removeStaleEndpoint(ctx context.Context, path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !ownedSocket(info) {
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

	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return listener, cleanup, nil
}
