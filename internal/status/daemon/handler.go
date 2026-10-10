package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
)

func (d *daemon) handle(parent context.Context, conn *net.UnixConn) {
	defer d.logs.recordPanic()
	defer conn.Close()

	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stop()

	var req request
	if err := readFrame(conn, maxRequestBytes, &req); err != nil {
		if ctx.Err() == nil && !errors.Is(err, io.EOF) {
			d.logs.repeatedFailure("request_read_failed", err)
		}
		return
	}

	if req.Version != protocolVersion || req.Server != d.binding.Server {
		d.logs.repeatedFailure("request_rejected", fmt.Errorf("version or server identity mismatch"))
		return
	}

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

	row, ok := d.respond(ctx, req.Action)
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

func (d *daemon) respond(ctx context.Context, action string) (reply, bool) {
	row := reply{
		Version: protocolVersion,
		Server:  d.binding.Server,
		PID:     d.info.PID,
		Epoch:   d.info.Epoch,
	}

	switch action {
	case "ping", "stop":
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
