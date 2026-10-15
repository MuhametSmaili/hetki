package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

type statusReaderFunc func(context.Context) (status.Snapshot, error)

func (f statusReaderFunc) ReadStatus(ctx context.Context) (status.Snapshot, error) {
	return f(ctx)
}

func TestReadStatusAbsenceAndReplacement(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	previous, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Working, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	for _, reader := range []StatusReader{
		nil,
		statusReaderFunc(func(context.Context) (status.Snapshot, error) { return status.Snapshot{}, nil }),
	} {
		result, err := ReadStatus(context.Background(), reader, previous)
		require.NoError(t, err)
		require.Equal(t, status.Unreported, result.At(pane, now))
	}
	next, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: {State: status.Withdrawn}})
	require.NoError(t, err)
	result, err := ReadStatus(context.Background(), statusReaderFunc(func(context.Context) (status.Snapshot, error) {
		return next, nil
	}), previous)
	require.NoError(t, err)
	require.Equal(t, status.Withdrawn, result.At(pane, now))
	require.Equal(t, status.Working, previous.At(pane, now))
}

func TestReadStatusFailureDiscardsPartialResult(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	active := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	cleared := status.PaneRef{Server: "server", ID: "%2", Generation: "birth"}
	unseen := status.PaneRef{Server: "server", ID: "%3", Generation: "birth"}
	previous, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		active:  {State: status.Blocked, ValidUntil: now.Add(time.Minute)},
		cleared: {State: status.Withdrawn},
	})
	require.NoError(t, err)
	partial, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		unseen: {State: status.Idle, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	wantErr := errors.New("source unavailable")
	reader := statusReaderFunc(func(context.Context) (status.Snapshot, error) { return partial, wantErr })
	result, err := ReadStatus(context.Background(), reader, previous)
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, status.Unknown, result.At(active, now))
	require.Equal(t, status.Withdrawn, result.At(cleared, now))
	require.Equal(t, status.Unreported, result.At(unseen, now))
	require.Equal(t, status.Blocked, previous.At(active, now))
}

func TestReadStatusCancellation(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	previous, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Idle, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	for _, cancelBefore := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelBefore {
			cancel()
		}
		calls := 0
		reader := statusReaderFunc(func(got context.Context) (status.Snapshot, error) {
			calls++
			require.Same(t, ctx, got)
			cancel()
			return previous, nil
		})
		result, err := ReadStatus(ctx, reader, previous)
		cancel()
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, status.Unknown, result.At(pane, now))
		if cancelBefore {
			require.Zero(t, calls)
		} else {
			require.Equal(t, 1, calls)
		}
	}
}
