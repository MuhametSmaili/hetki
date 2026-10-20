package daemon

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
)

// subscriptionIdleTimeout is the longest wait for a frame. The daemon sends a
// frame at each change, and the inventory changes about every two seconds. A
// longer silence means that the connection is broken.
const subscriptionIdleTimeout = 30 * time.Second

// Subscription receives the views that the daemon pushes. Call Next from one
// goroutine only. Close can run at any time and stops a blocked Next.
type Subscription struct {
	client *Client
	conn   net.Conn
}

// Subscribe opens a connection that receives the current view, then a new view
// at each change.
func (c *Client) Subscribe(ctx context.Context) (*Subscription, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})

	err = writeFrame(conn, maxRequestBytes, request{Version: protocolVersion, Server: c.server, Action: "subscribe"})
	if !stop() {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}

	_ = conn.SetDeadline(time.Time{})
	return &Subscription{client: c, conn: conn}, nil
}

// Next waits for the next view. ErrUnavailable means that the daemon has no
// valid view now; the subscription stays open. Other errors end it.
func (s *Subscription) Next() (View, error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(subscriptionIdleTimeout))

	row, err := s.client.readReply(s.conn)
	if err != nil {
		return View{}, err
	}

	return s.client.decodeView(row)
}

func (s *Subscription) Close() error {
	return s.conn.Close()
}

// subscribe writes the current view, then the latest view after each change.
// Changes that occur during a write are combined into the next frame.
func (d *daemon) subscribe(ctx context.Context, conn *net.UnixConn) {
	select {
	case d.subscribers <- struct{}{}:
		defer func() { <-d.subscribers }()
	default:
		d.logs.repeatedFailure("subscription_rejected", errors.New("subscriber limit reached"))
		return
	}

	_ = conn.SetDeadline(time.Time{})
	for ctx.Err() == nil {
		changes := d.source.Changes()

		row := d.identity()
		d.readView(ctx, &row)
		if !d.checkLifetime(ctx) {
			return
		}

		_ = conn.SetWriteDeadline(time.Now().Add(requestTimeout))
		if err := writeFrame(conn, maxReplyBytes, row); err != nil {
			if ctx.Err() == nil && !clientGone(err) {
				d.logs.repeatedFailure("reply_write_failed", err)
			}
			return
		}

		select {
		case <-ctx.Done():
		case <-changes:
		}
	}
}

func (d *daemon) readView(ctx context.Context, row *reply) {
	view, err := d.source.ReadView(ctx)
	if err == nil {
		err = d.encodeView(row, view)
	}
	if err == nil {
		return
	}

	if ctx.Err() == nil {
		d.logs.repeatedFailure("source_read_failed", err)
	}
	row.Entries, row.Locations, row.Error = nil, nil, "unavailable"
}

func clientGone(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed)
}
