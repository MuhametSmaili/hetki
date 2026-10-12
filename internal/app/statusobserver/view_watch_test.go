package statusobserver

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/daemon"
	"github.com/stretchr/testify/require"
)

func TestWatchViewWaitsForProtocolChangesAndCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, row := sourceFixture(t)
		scope := sourceScope(row.facts)
		source.Reset(scope)
		require.NoError(t, source.Output(scope, []byte("\x1b]7501;state=idle\a")))
		initial, err := source.WatchView(t.Context(), 0)
		require.NoError(t, err)
		views := make(chan daemon.View, 1)
		errors := make(chan error, 1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			view, err := source.WatchView(ctx, initial.Cursor.Revision)
			views <- view
			errors <- err
		}()
		synctest.Wait()

		require.NoError(t, source.Output(scope, []byte("\x1b]7501;state=idle\a")))
		synctest.Wait()
		select {
		case <-views:
			t.Fatal("identical protocol evidence woke the watch")
		default:
		}
		require.NoError(t, source.Output(scope, []byte("\x1b]7501;state=blocked\a")))
		synctest.Wait()

		updated := <-views
		require.NoError(t, <-errors)
		require.Equal(t, status.Blocked, updated.Values.At(row.pane.Ref, time.Now()))
		require.Greater(t, updated.Cursor.Revision, initial.Cursor.Revision)
		cancel()
		_, err = source.WatchView(ctx, updated.Cursor.Revision)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestWatchViewCancellationUnblocksAllWaiters(t *testing.T) {
	cases := []struct {
		name       string
		stopSource bool
		wantErr    error
	}{
		{name: "request canceled", wantErr: context.Canceled},
		{name: "source stopped", stopSource: true, wantErr: daemon.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				source, _ := sourceFixture(t)
				initial, err := source.WatchView(t.Context(), 0)
				require.NoError(t, err)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				finished := make(chan error, 32)
				for range cap(finished) {
					go func() {
						_, err := source.WatchView(ctx, initial.Cursor.Revision)
						finished <- err
					}()
				}
				synctest.Wait()

				if tc.stopSource {
					source.cancel()
				} else {
					cancel()
				}
				synctest.Wait()

				require.Len(t, finished, cap(finished))
				for range cap(finished) {
					require.ErrorIs(t, <-finished, tc.wantErr)
				}
			})
		})
	}
}
