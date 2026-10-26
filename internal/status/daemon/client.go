package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

type Client struct {
	server string
	path   string
}

// Info identifies one instance. Build and Executable are empty for an instance
// that speaks another protocol; Executable is also empty when the instance
// could not record its binary.
type Info struct {
	PID        int
	Epoch      string
	Build      string
	Executable string
}

func Connect(server string) (*Client, error) {
	path, err := endpoint(server)
	if err != nil {
		return nil, err
	}

	if err := prepareEndpoint(path); err != nil {
		return nil, err
	}

	return &Client{server: server, path: path}, nil
}

func (c *Client) call(ctx context.Context, action string) (reply, error) {
	return c.callRequest(ctx, request{Version: protocolVersion, Server: c.server, Action: action}, requestTimeout)
}

func (c *Client) callRequest(ctx context.Context, req request, timeout time.Duration) (reply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := c.dial(ctx)
	if err != nil {
		return reply{}, err
	}
	defer conn.Close()

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stop()

	result, err := c.exchangeRequest(conn, req)
	if ctx.Err() != nil {
		return reply{}, ctx.Err()
	}

	if errors.Is(err, io.EOF) {
		return reply{}, fmt.Errorf("%w (it may be shutting down or from an older hetki build): %w", errNoReply, err)
	}

	return result, err
}

// dial checks the private directory and that the endpoint is a socket owned by
// this user before it connects.
func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := validateEndpointDirectory(c.path); err != nil {
		return nil, err
	}

	info, err := os.Lstat(c.path)
	if err != nil {
		return nil, err
	}

	if !ownedSocket(info) {
		return nil, fmt.Errorf("unsafe status daemon endpoint")
	}

	return (&net.Dialer{}).DialContext(ctx, "unix", c.path)
}

func (c *Client) exchange(conn net.Conn, action string) (reply, error) {
	return c.exchangeRequest(conn, request{Version: protocolVersion, Server: c.server, Action: action})
}

func (c *Client) exchangeRequest(conn net.Conn, req request) (reply, error) {
	if err := writeFrame(conn, maxRequestBytes, req); err != nil {
		return reply{}, err
	}

	return c.readReply(conn)
}

func (c *Client) readReply(conn net.Conn) (reply, error) {
	var result reply
	if err := readFrame(conn, maxReplyBytes, &result); err != nil {
		return reply{}, err
	}

	if result.Version != protocolVersion {
		return reply{}, &ProtocolMismatchError{PID: max(result.PID, 0), Epoch: result.Epoch, Version: result.Version}
	}

	if result.Server != c.server || result.PID <= 0 || !validEpoch(result.Epoch) {
		return reply{}, ErrIdentity
	}

	switch result.Error {
	case "instance_changed":
		return reply{}, ErrIdentity
	case "", busyReply:
		if !validIdentity(result.Build) || len(result.Executable) > maxPathBytes {
			return reply{}, ErrIdentity
		}
	default:
		return reply{}, ErrUnavailable
	}

	if result.Error == busyReply {
		return result, ErrBusy
	}
	return result, nil
}

func (c *Client) Ping(ctx context.Context) (Info, error) {
	result, err := c.call(ctx, "ping")
	return Info{PID: result.PID, Epoch: result.Epoch, Build: result.Build, Executable: result.Executable}, err
}
