package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

// State classifies the status service of one server at a single probe. The
// zero value is not a state, so an unset result is never mistaken for absence.
type State uint8

const (
	// Absent means that neither a listener nor a locked owner was observed.
	// It does not exclude a concurrent start.
	Absent State = iota + 1

	// Transition means that an instance owns the endpoint but is not serving,
	// because it is starting or stopping.
	Transition

	// Running means that an instance answered a ping.
	Running
)

// observation is one classification of the endpoint. Info is set only for
// Running; cause explains a Transition.
type observation struct {
	state State
	info  Info
	cause error
}

// probe classifies the endpoint once without creating, repairing or removing
// files. Protocol, identity and unsafe filesystem failures remain errors.
func (c *Client) probe(ctx context.Context) (observation, error) {
	if err := ctx.Err(); err != nil {
		return observation{}, err
	}

	if err := validateEndpointDirectory(c.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return observation{state: Absent}, nil
		}
		return observation{}, err
	}

	info, err := c.Ping(ctx)
	if err == nil || errors.Is(err, ErrBusy) {
		return observation{state: Running, info: info}, nil
	}
	if interrupted(err) {
		return observation{state: Transition, cause: err}, nil
	}
	if !isMissingEndpoint(err) {
		return observation{}, err
	}

	owned, lockErr := endpointOwned(c.path)
	if lockErr != nil {
		return observation{}, lockErr
	}
	if owned {
		return observation{state: Transition, cause: err}, nil
	}
	return observation{state: Absent}, nil
}

// settle probes until the endpoint leaves Transition, honoring ctx. With an
// error, it returns the last Transition observed so that callers can explain
// why waiting failed.
func (c *Client) settle(ctx context.Context) (observation, error) {
	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()

	var last observation
	for {
		current, err := c.probe(ctx)
		if err != nil {
			return last, err
		}
		if current.state != Transition {
			return current, nil
		}
		last = current

		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-ticker.C:
		}
	}
}

// interrupted reports a listener that cannot answer anymore: shutdown closes
// accepted connections. A daemon far beyond its client limit also closes
// connections without a busy reply; retrying suits that case too.
func interrupted(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}
