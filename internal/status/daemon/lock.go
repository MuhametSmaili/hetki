package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const lifecyclePollInterval = 20 * time.Millisecond

var errLockChanged = errors.New("status daemon lock path is no longer valid")

// Every owner uses this same inode. The fixed-size epoch changes only after a
// successor acquires the lock, so a change proves the previous owner's teardown.
// The file is never unlinked, including after an owner exits.
type endpointLock struct {
	file *os.File
}

func openEndpointLock(path string, flags int) (*endpointLock, error) {
	file, err := os.OpenFile(path+".lock", flags|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}

	lock := &endpointLock{file: file}
	if err := lock.checkPath(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return lock, nil
}

func (l *endpointLock) Close() error { return l.file.Close() }

func (l *endpointLock) checkPath() error {
	if err := validateEndpointDirectory(l.file.Name()); err != nil {
		return errors.Join(errLockChanged, err)
	}

	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || !ownedMode(info, 0600) || stat.Nlink != 1 {
		return fmt.Errorf("unsafe status daemon lock")
	}

	current, err := os.Lstat(l.file.Name())
	if err != nil {
		return errors.Join(errLockChanged, err)
	}
	if !os.SameFile(info, current) {
		return errLockChanged
	}
	return nil
}

func (l *endpointLock) acquire() (bool, error) {
	if err := l.checkPath(); err != nil {
		return false, err
	}

	err := unix.Flock(int(l.file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, l.checkPath()
}

// A nil lock means another process owns it; callers never wait while holding it.
func lockEndpoint(path string) (*endpointLock, error) {
	lock, err := openEndpointLock(path, os.O_CREATE)
	if err != nil {
		return nil, err
	}
	acquired, err := lock.acquire()
	if err != nil || !acquired {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

func (l *endpointLock) setOwner(epoch string) error {
	n, err := l.file.WriteAt([]byte(epoch), 0)
	if err != nil {
		return err
	}
	if n != len(epoch) {
		return io.ErrShortWrite
	}
	return l.file.Truncate(int64(len(epoch)))
}

func (l *endpointLock) owner() (string, error) {
	var data [33]byte
	n, err := l.file.ReadAt(data[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if n != 32 || !validEpoch(string(data[:n])) {
		return "", ErrLifecycle
	}
	return string(data[:n]), nil
}

func (l *endpointLock) waitStopped(ctx context.Context, epoch string) error {
	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		acquired, err := l.acquire()
		if err != nil || acquired {
			return err
		}

		owner, err := l.owner()
		if err == nil && owner != epoch {
			return l.checkPath()
		}
		if err != nil && !errors.Is(err, ErrLifecycle) {
			return err
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for daemon cleanup: %w", errors.Join(ctx.Err(), err))
		case <-ticker.C:
		}
	}
}

func endpointOwned(path string) (owned bool, err error) {
	lock, err := openEndpointLock(path, 0)
	if errors.Is(err, os.ErrNotExist) && !errors.Is(err, errLockChanged) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	acquired, err := lock.acquire()
	return !acquired, err
}
