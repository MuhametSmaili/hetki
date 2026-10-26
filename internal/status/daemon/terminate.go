package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"
)

// mismatchReply holds the fields that every protocol version sends when it
// rejects another version. Decoding is lenient so that future versions can
// still be identified.
type mismatchReply struct {
	Version int    `json:"version"`
	Server  string `json:"server"`
	PID     int    `json:"pid"`
	Epoch   string `json:"epoch"`
	Error   string `json:"error"`
}

// terminateIncompatible stops the observed instance that speaks another
// protocol and so cannot accept a stop request. It signals only a process that
// the kernel names as the endpoint's listener, that reports the observed epoch,
// and whose epoch owns the held endpoint lock. ErrIdentity means that the
// instance changed after it was observed; callers probe again.
func (c *Client) terminateIncompatible(ctx context.Context, observed Info) (err error) {
	lock, err := openEndpointLock(c.path, 0)
	if err != nil {
		return fmt.Errorf("open incompatible daemon lock: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	target, err := c.identifyIncompatible(ctx)
	if err != nil {
		return err
	}
	if target.Epoch != observed.Epoch {
		return fmt.Errorf("incompatible daemon instance changed: %w", ErrIdentity)
	}

	if err := requireLockOwner(lock, target.Epoch); err != nil {
		return err
	}

	if err := syscall.Kill(target.PID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal incompatible daemon PID %d: %w", target.PID, err)
	}

	if err := lock.waitStopped(ctx, target.Epoch); err != nil {
		return fmt.Errorf("incompatible daemon PID %d did not stop after SIGTERM: %w", target.PID, err)
	}
	return nil
}

// identifyIncompatible pings the endpoint and returns the PID and epoch of an
// instance that rejects this protocol, after the kernel confirms the PID.
func (c *Client) identifyIncompatible(ctx context.Context) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	conn, err := c.dial(ctx)
	if err != nil {
		return Info{}, err
	}
	defer conn.Close()

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)

	listener, err := peerPID(conn)
	if err != nil {
		return Info{}, fmt.Errorf("identify daemon socket owner: %w", err)
	}

	row, err := c.readMismatch(conn)
	if err != nil {
		return Info{}, err
	}

	if row.PID != listener {
		return Info{}, fmt.Errorf("daemon reports PID %d, but PID %d owns its socket: %w", row.PID, listener, ErrIdentity)
	}
	return Info{PID: row.PID, Epoch: row.Epoch}, nil
}

func (c *Client) readMismatch(conn net.Conn) (mismatchReply, error) {
	if err := writeFrame(conn, maxRequestBytes, request{Version: protocolVersion, Server: c.server, Action: "ping"}); err != nil {
		return mismatchReply{}, err
	}

	raw, err := readFrameData(conn, maxReplyBytes)
	if err != nil {
		return mismatchReply{}, fmt.Errorf("read incompatible daemon identity: %w", err)
	}

	var row mismatchReply
	if err := json.Unmarshal(raw, &row); err != nil {
		return mismatchReply{}, fmt.Errorf("decode incompatible daemon identity: %w", err)
	}

	if row.Version == protocolVersion {
		return mismatchReply{}, fmt.Errorf("daemon now speaks this protocol: %w", ErrIdentity)
	}
	if row.Error != protocolMismatch || row.Server != c.server || row.PID <= 0 || !validEpoch(row.Epoch) {
		return mismatchReply{}, fmt.Errorf("incompatible daemon reply: %w", ErrIdentity)
	}
	return row, nil
}

// requireLockOwner confirms that a live process holds the lock for epoch. An
// acquired lock is released when the caller closes it.
func requireLockOwner(lock *endpointLock, epoch string) error {
	owner, err := lock.owner()
	if err != nil {
		return err
	}
	if owner != epoch {
		return fmt.Errorf("daemon lock owner changed: %w", ErrIdentity)
	}

	acquired, err := lock.acquire()
	if err != nil {
		return err
	}
	if acquired {
		return fmt.Errorf("no process holds the daemon lock: %w", ErrIdentity)
	}
	return nil
}

// socketOption reads an integer property of a Unix socket's descriptor.
func socketOption(conn net.Conn, read func(fd int) (int, error)) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("daemon connection is not a Unix socket")
	}

	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}

	var value int
	var readErr error
	if err := raw.Control(func(fd uintptr) {
		value, readErr = read(int(fd))
	}); err != nil {
		return 0, err
	}
	return value, readErr
}
