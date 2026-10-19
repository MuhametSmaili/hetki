package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
)

type Client struct {
	server string
	path   string
}

type Info struct {
	PID   int
	Epoch string
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

	if err := ctx.Err(); err != nil {
		return reply{}, err
	}

	info, err := os.Lstat(c.path)
	if err != nil {
		return reply{}, err
	}

	if info.Mode()&os.ModeSocket == 0 || !ownedMode(info, 0600) {
		return reply{}, fmt.Errorf("unsafe status daemon endpoint")
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.path)
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

	return result, err
}

func (c *Client) exchange(conn net.Conn, action string) (reply, error) {
	return c.exchangeRequest(conn, request{Version: protocolVersion, Server: c.server, Action: action})
}

func (c *Client) exchangeRequest(conn net.Conn, req request) (reply, error) {
	if err := writeFrame(conn, maxRequestBytes, req); err != nil {
		return reply{}, err
	}

	var result reply
	if err := readFrame(conn, maxReplyBytes, &result); err != nil {
		return reply{}, err
	}

	if result.Version != protocolVersion {
		return reply{}, ErrProtocol
	}

	epoch, err := hex.DecodeString(result.Epoch)
	if result.Server != c.server || result.PID <= 0 || err != nil || len(epoch) != 16 {
		return reply{}, ErrIdentity
	}

	if result.Error != "" {
		return reply{}, ErrUnavailable
	}

	return result, nil
}

func (c *Client) Ping(ctx context.Context) (Info, error) {
	result, err := c.call(ctx, "ping")
	return Info{PID: result.PID, Epoch: result.Epoch}, err
}

func (c *Client) ReadStatus(ctx context.Context) (status.Snapshot, error) {
	result, err := c.call(ctx, "snapshot")
	if err != nil {
		return status.Snapshot{}, err
	}

	return result.snapshot()
}

func (c *Client) Stop(ctx context.Context) error {
	_, err := c.call(ctx, "stop")
	return err
}
