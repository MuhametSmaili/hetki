package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	watchWait = 20 * time.Second

	// WatchTimeout includes the bounded source wait and transport budget.
	WatchTimeout = watchWait + requestTimeout
	maxWatchers  = maxClients / 2
)

// Cursor identifies a complete view within one daemon lifetime. Zero requests
// the current view immediately; clients never replay individual transitions.
type Cursor struct {
	Epoch    string `json:"epoch"`
	Revision uint64 `json:"revision"`
}

type viewWatcher interface {
	ReadView(context.Context) (View, error)
	WatchView(context.Context, uint64) (View, error)
}

// SupportsWatch probes separately so existing v1 ping/view replies stay
// compatible. Older development daemons close unknown-action requests.
func (c *Client) SupportsWatch(ctx context.Context) (bool, error) {
	row, err := c.call(ctx, "capabilities")
	if errors.Is(err, io.EOF) {
		return false, nil
	}

	return row.Watch, err
}

// WatchView returns the latest view on change or after a bounded renewal wait.
// Cancellation closes the connection and releases the server's waiter slot.
func (c *Client) WatchView(ctx context.Context, after Cursor) (View, error) {
	req := request{Version: protocolVersion, Server: c.server, Action: "watch", After: &after}
	row, err := c.callRequest(ctx, req, WatchTimeout)
	if err != nil {
		return View{}, err
	}

	if row.Revision == 0 {
		return View{}, fmt.Errorf("%w: missing watch revision", ErrProtocol)
	}

	return c.decodeView(row)
}

func (d *daemon) readWatchedView(ctx context.Context, after *Cursor, row *reply) error {
	source, ok := d.source.(viewWatcher)
	if !ok || after == nil {
		return ErrUnavailable
	}

	select {
	case d.watchers <- struct{}{}:
		defer func() { <-d.watchers }()
	default:
		return ErrUnavailable
	}

	var revision uint64
	if after.Epoch == d.info.Epoch {
		revision = after.Revision
	}

	waitCtx, cancel := context.WithTimeout(ctx, watchWait)
	defer cancel()

	view, err := source.WatchView(waitCtx, revision)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		view, err = source.ReadView(ctx)
	}
	if err != nil {
		return err
	}

	if view.Cursor.Revision == 0 {
		return fmt.Errorf("source returned an unversioned watch view")
	}

	row.Revision = view.Cursor.Revision
	return d.encodeView(row, view)
}
