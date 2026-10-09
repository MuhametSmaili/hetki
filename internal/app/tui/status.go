package tui

import (
	"context"

	"github.com/MSmaili/hetki/internal/status"
)

// StatusReader returns complete snapshots without collecting new evidence.
type StatusReader interface {
	ReadStatus(context.Context) (status.Snapshot, error)
}

func ReadStatus(ctx context.Context, reader StatusReader, previous status.Snapshot) (status.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return previous.Invalidate(), err
	}
	if reader == nil {
		return status.Snapshot{}, nil
	}
	snapshot, err := reader.ReadStatus(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return previous.Invalidate(), err
	}
	return snapshot, nil
}
