package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"syscall"
	"time"
)

// subscriptionIdleTimeout is the longest wait for a frame. The daemon sends a
// frame at each change and at each inventory renewal, about every two seconds.
// A longer silence means that the connection is broken.
const subscriptionIdleTimeout = 30 * time.Second

// Subscription receives the views that the daemon pushes. Call Next from one
// goroutine only. Close can run at any time and stops a blocked Next.
type Subscription struct {
	client *Client
	conn   net.Conn

	// last is the latest view before the lease is applied.
	last    View
	hasLast bool
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

// Next waits for the next view and applies the lease to it. ErrUnavailable
// means that the daemon has no valid view now, but the subscription stays open.
// Other errors end it.
func (s *Subscription) Next() (View, error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(subscriptionIdleTimeout))

	row, err := s.client.readReply(s.conn)
	if err != nil {
		s.hasLast = false
		return View{}, err
	}

	var view View
	switch {
	case !row.Renewal:
		view, err = s.client.decodeView(row)
	case s.hasLast:
		view, err = s.client.renew(s.last, row)
	default:
		err = fmt.Errorf("%w: renewal without a full view", ErrProtocol)
	}
	if err != nil {
		return View{}, err
	}

	s.last, s.hasLast = view, true
	view.Values = view.Values.Leased(view.Lease)
	view.Locations = slices.Clone(view.Locations)
	return view, nil
}

func (s *Subscription) Close() error {
	return s.conn.Close()
}

// subscribe writes the current view, then the latest view after each change.
// Changes that occur during a write go into the next frame. If only the lease
// or deadlines moved, it writes a renewal frame.
func (d *daemon) subscribe(ctx context.Context, conn *net.UnixConn) {
	select {
	case d.subscribers <- struct{}{}:
		defer func() { <-d.subscribers }()
	default:
		d.logs.repeatedFailure("subscription_rejected", errors.New("subscriber limit reached"))
		d.writeBusy(ctx, conn)
		return
	}

	_ = conn.SetDeadline(time.Time{})
	ctx, cancel := context.WithCancel(ctx)
	disconnected := make(chan struct{})
	go func() {
		d.waitForSubscriberClose(ctx, conn)
		cancel()
		_ = conn.Close()
		close(disconnected)
	}()
	defer func() {
		cancel()
		_ = conn.Close()
		<-disconnected
	}()

	var frames frameEncoder
	for ctx.Err() == nil {
		changes := d.source.Changes()
		if !d.sendFrame(ctx, conn, &frames) {
			return
		}

		select {
		case <-ctx.Done():
		case <-changes:
		}
	}
}

// Subscribers send no more input after the request. Reading one byte detects
// closure even when the source is quiet, without buffering unsolicited input.
func (d *daemon) waitForSubscriberClose(ctx context.Context, conn *net.UnixConn) {
	var input [1]byte
	n, err := conn.Read(input[:])
	if ctx.Err() != nil {
		return
	}

	if n > 0 {
		d.logs.repeatedFailure("request_rejected", errors.New("unexpected subscription input"))
		return
	}

	if err != nil && !errors.Is(err, io.EOF) && !clientGone(err) {
		d.logs.repeatedFailure("request_read_failed", err)
	}
}

// sendFrame writes one frame. It reports false when the subscription must end.
func (d *daemon) sendFrame(ctx context.Context, conn *net.UnixConn, frames *frameEncoder) bool {
	row := d.identity()
	if err := frames.encode(ctx, d, &row); err != nil {
		if ctx.Err() == nil {
			d.logs.repeatedFailure("source_read_failed", err)
		}
		row.Error = "unavailable"
	}

	if !d.checkLifetime(ctx) {
		return false
	}

	_ = conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	if err := writeFrame(conn, maxReplyBytes, row); err != nil {
		if ctx.Err() == nil && !clientGone(err) {
			d.logs.repeatedFailure("reply_write_failed", err)
		}
		return false
	}
	return true
}

// frameEncoder remembers the last view sent to one subscriber.
type frameEncoder struct {
	sent    View
	hasSent bool
}

// encode reads the current view into row. If the subscriber already has the
// same evidence, it writes a renewal. After an error, the next frame is full.
func (f *frameEncoder) encode(ctx context.Context, d *daemon, row *reply) error {
	view, err := d.source.ReadView(ctx)
	if err == nil {
		err = view.validate(d.binding.Server)
	}

	switch {
	case err != nil:
	case f.hasSent && sameEvidence(f.sent, view):
		encodeRenewal(row, f.sent, view)
	default:
		err = d.encodeView(row, view)
	}

	f.sent, f.hasSent = view, err == nil
	return err
}

func clientGone(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed)
}
