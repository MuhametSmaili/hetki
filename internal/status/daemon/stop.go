package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const shutdownTimeout = 5 * time.Second

// Stop targets one observed instance without creating storage or starting a
// daemon. It first waits, within its timeout, for a Transition to settle, and
// returns ErrTransition if it does not. A nil instance means the service settled
// as Absent, not that a concurrent starter is excluded. Success waits for the
// target's workers, source and logs to close; it does not wait for a
// replacement to exit.
func Stop(ctx context.Context, server string) (*Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := endpoint(server)
	if err != nil {
		return nil, err
	}
	return (&Client{server: server, path: path}).stop(ctx)
}

// Stop requests and waits for the current instance's teardown. Cancellation or a
// timeout after acceptance does not undo shutdown. It never stops a replacement.
func (c *Client) Stop(ctx context.Context) error {
	_, err := c.stop(ctx)
	return err
}

func (c *Client) stop(ctx context.Context) (*Info, error) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	observed, err := c.settle(ctx)
	if err != nil {
		if observed.state == Transition && ctx.Err() != nil {
			err = errors.Join(ErrTransition, err, observed.cause)
		}
		return nil, err
	}
	if observed.state == Absent {
		return nil, nil
	}

	if err := c.stopObserved(ctx, observed.info); err != nil {
		return nil, err
	}
	return &observed.info, nil
}

func (c *Client) stopObserved(ctx context.Context, info Info) (err error) {
	lock, err := openEndpointLock(c.path, 0)
	if err != nil {
		return fmt.Errorf("open observed daemon lock: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	owner, err := lock.owner()
	if err != nil {
		return err
	}
	if owner != info.Epoch {
		return fmt.Errorf("daemon lock owner changed: %w", ErrIdentity)
	}

	if err := c.requestStop(ctx, info.Epoch); err != nil {
		return err
	}
	return lock.waitStopped(ctx, info.Epoch)
}

func (c *Client) requestStop(ctx context.Context, epoch string) error {
	if !validEpoch(epoch) {
		return ErrIdentity
	}
	row, err := c.callRequest(ctx, request{
		Version: protocolVersion, Server: c.server, Action: "stop", ExpectedEpoch: epoch,
	}, requestTimeout)
	if err != nil {
		return fmt.Errorf("request instance-safe stop (no legacy fallback): %w", err)
	}
	if row.Epoch != epoch {
		return ErrIdentity
	}
	return nil
}
