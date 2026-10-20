package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

func (d *daemon) handle(parent context.Context, conn *net.UnixConn) {
	defer d.logs.recordPanic()
	defer conn.Close()

	stopParent := context.AfterFunc(parent, func() {
		_ = conn.Close()
	})
	defer stopParent()

	req, ok := d.readRequest(parent, conn)
	if !ok {
		return
	}

	switch req.Action {
	case "subscribe":
		d.subscribe(parent, conn)
	case "ping", "stop":
		d.control(parent, conn, req)
	default:
		d.logs.repeatedFailure("request_rejected", fmt.Errorf("unknown action"))
	}
}

func (d *daemon) control(parent context.Context, conn *net.UnixConn, req request) {
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)

	if !d.checkLifetime(ctx) {
		return
	}

	if err := d.publish(ctx, conn, req, d.identity()); err != nil && ctx.Err() == nil {
		d.logs.repeatedFailure("reply_write_failed", err)
	}
}

// Serialize readiness publication with stop acceptance, not with source reads.
// A successful Ping may precede a stop, but cannot be published after acceptance.
// Accepted shutdown survives a lost acknowledgment.
func (d *daemon) publish(ctx context.Context, conn *net.UnixConn, req request, row reply) error {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if req.Action == "stop" {
		if req.ExpectedEpoch != d.info.Epoch {
			row.Error = "instance_changed"
		} else {
			d.logs.info("stop_requested")
			defer d.cancel()
		}
	}
	return writeFrame(conn, maxReplyBytes, row)
}

func (d *daemon) readRequest(ctx context.Context, conn *net.UnixConn) (request, bool) {
	_ = conn.SetDeadline(time.Now().Add(requestTimeout))
	var req request
	if err := readFrame(conn, maxRequestBytes, &req); err != nil {
		if ctx.Err() == nil && !errors.Is(err, io.EOF) {
			d.logs.repeatedFailure("request_read_failed", err)
		}
		return request{}, false
	}

	if req.Version != protocolVersion || req.Server != d.binding.Server {
		d.logs.repeatedFailure("request_rejected", fmt.Errorf("version or server identity mismatch"))
		return request{}, false
	}

	if (req.Action == "stop" && !validEpoch(req.ExpectedEpoch)) ||
		(req.Action != "stop" && req.ExpectedEpoch != "") {
		d.logs.repeatedFailure("request_rejected", fmt.Errorf("invalid expected instance"))
		return request{}, false
	}

	return req, true
}

func (d *daemon) checkLifetime(ctx context.Context) bool {
	if bindingAlive(ctx, d.binding) {
		return true
	}

	if ctx.Err() == nil {
		d.logs.info("backend_unavailable")
		d.cancel()
	}

	return false
}

func (d *daemon) identity() reply {
	return reply{
		Version: protocolVersion,
		Server:  d.binding.Server,
		PID:     d.info.PID,
		Epoch:   d.info.Epoch,
	}
}
