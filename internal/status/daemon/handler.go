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

	timeout := requestTimeout
	if req.Action == "watch" {
		timeout = WatchTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stop()

	disconnected := make(chan struct{})
	go func() {
		defer close(disconnected)

		var extra [1]byte
		_, _ = conn.Read(extra[:])
		cancel()
	}()
	defer func() {
		_ = conn.Close()
		<-disconnected
	}()

	if !d.checkLifetime(ctx) {
		return
	}

	row, ok := d.respond(ctx, req)
	if !ok || ctx.Err() != nil {
		return
	}

	if !d.checkLifetime(ctx) {
		return
	}

	if err := writeFrame(conn, maxReplyBytes, row); err != nil {
		if ctx.Err() == nil {
			d.logs.repeatedFailure("reply_write_failed", err)
		}
		return
	}

	if req.Action == "stop" {
		d.logs.info("stop_requested")
		d.cancel()
	}
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

	if req.After != nil && (req.Action != "watch" || len(req.After.Epoch) > maxIdentityBytes) {
		d.logs.repeatedFailure("request_rejected", fmt.Errorf("invalid watch cursor"))
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

func (d *daemon) respond(ctx context.Context, req request) (reply, bool) {
	row := reply{
		Version: protocolVersion,
		Server:  d.binding.Server,
		PID:     d.info.PID,
		Epoch:   d.info.Epoch,
	}

	switch req.Action {
	case "capabilities":
		_, row.Watch = d.source.(viewWatcher)
	case "ping", "stop":
	case "view", "watch":
		if err := d.requestView(ctx, req, &row); err != nil {
			if ctx.Err() == nil {
				d.logs.repeatedFailure("source_read_failed", err)
			}
			row.Entries, row.Locations, row.Error = nil, nil, "unavailable"
		}
	case "snapshot":
		snapshot, err := d.source.ReadStatus(ctx)
		if err == nil {
			row.Entries, err = snapshotEntries(d.binding.Server, snapshot)
		}

		if err != nil {
			if ctx.Err() == nil {
				d.logs.repeatedFailure("source_read_failed", err)
			}
			row.Entries, row.Error = nil, "unavailable"
		}

	default:
		d.logs.repeatedFailure("request_rejected", fmt.Errorf("unknown action"))
		return reply{}, false
	}

	return row, true
}

func (d *daemon) requestView(ctx context.Context, req request, row *reply) error {
	if req.Action == "watch" {
		return d.readWatchedView(ctx, req.After, row)
	}

	return d.readView(ctx, row)
}
