//go:build integration

package statusobserver

import (
	"context"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
)

// readDaemonStatus subscribes, reads the first view and closes the subscription.
func readDaemonStatus(ctx context.Context, client *daemon.Client) (status.Snapshot, error) {
	subscription, err := client.Subscribe(ctx)
	if err != nil {
		return status.Snapshot{}, err
	}
	defer subscription.Close()

	stop := context.AfterFunc(ctx, func() {
		_ = subscription.Close()
	})
	defer stop()

	view, err := subscription.Next()
	if ctx.Err() != nil {
		return status.Snapshot{}, ctx.Err()
	}
	return view.Values, err
}
