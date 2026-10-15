package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func firstPaneRef(t *testing.T, values status.Snapshot) status.PaneRef {
	t.Helper()
	for ref := range values.All() {
		return ref
	}
	t.Fatal("expected a reported pane")
	return status.PaneRef{}
}

func TestServiceReplacesStatusEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state status.State
	}{
		{"empty view", status.Unreported},
		{"withdrawn report", status.Withdrawn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, view := statusDeliveryFixture(t)
			pane := firstPaneRef(t, view.Values)
			next := daemon.View{}
			if tc.state != status.Unreported {
				var err error
				next.Values, err = status.NewSnapshot(map[status.PaneRef]status.Value{pane: {State: tc.state}})
				require.NoError(t, err)
				next.Locations = view.Locations
			}
			reads := 0
			service := Service{Driver: statusTestDriver{LiveAdapter: adapter, read: func(context.Context) (daemon.View, error) {
				reads++
				if reads == 1 {
					return view, nil
				}
				return next, nil
			}}}
			service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
				initial := options.Read(ctx, status.Snapshot{})
				acceptStatusUpdate(initial)

				result := options.Read(ctx, initial.Values)

				require.NoError(t, result.Err)
				require.Equal(t, tc.state, result.Values.At(pane, time.Now()))
				require.Equal(t, status.Blocked, initial.Values.At(pane, time.Now()))
				require.Equal(t, 2, reads)
				return list.ItemID{}, nil
			}

			require.NoError(t, service.Run(t.Context()))
		})
	}
}

func TestServiceDiscardsPartialStatusOnFailure(t *testing.T) {
	adapter, view := statusDeliveryFixture(t)
	active := firstPaneRef(t, view.Values)
	cleared, unseen := active, active
	cleared.ID, unseen.ID = "%2", "%3"
	previous, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		active:  view.Values.Lookup(active),
		cleared: {State: status.Withdrawn},
	})
	require.NoError(t, err)
	view.Values = previous
	partial, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		unseen: {State: status.Idle, ValidUntil: time.Now().Add(time.Minute)},
	})
	require.NoError(t, err)
	wantErr := errors.New("source unavailable")
	reads := 0
	service := Service{Driver: statusTestDriver{LiveAdapter: adapter, read: func(context.Context) (daemon.View, error) {
		reads++
		if reads == 1 {
			return view, nil
		}
		return daemon.View{Values: partial, Locations: []daemon.Location{{Session: "$1", Window: "@1", Pane: "%3"}}}, wantErr
	}}}
	service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
		initial := options.Read(ctx, status.Snapshot{})
		acceptStatusUpdate(initial)

		result := options.Read(ctx, initial.Values)

		require.ErrorIs(t, result.Err, wantErr)
		require.Nil(t, result.Bindings)
		require.Equal(t, status.Unknown, result.Values.At(active, time.Now()))
		require.Equal(t, status.Withdrawn, result.Values.At(cleared, time.Now()))
		require.Equal(t, status.Unreported, result.Values.At(unseen, time.Now()))
		require.Equal(t, status.Blocked, previous.At(active, time.Now()))
		return list.ItemID{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
}

func TestServiceCancelsStatusRead(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cancelBefore bool
		wantCalls    int
	}{
		{"before read", true, 0},
		{"during read", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, view := statusDeliveryFixture(t)
			calls := 0
			var cancelRead context.CancelFunc
			service := Service{Driver: statusTestDriver{LiveAdapter: adapter, read: func(context.Context) (daemon.View, error) {
				calls++
				cancelRead()
				return view, nil
			}}}
			service.RunUI = func(ctx context.Context, _ list.Snapshot, _ ui.KeyMap, _ ui.StartMode, _ ui.DispatchFunc, _ ui.PreviewOptions, options ui.StatusOptions) (list.ItemID, error) {
				readCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				cancelRead = cancel
				if tc.cancelBefore {
					cancel()
				}

				result := options.Read(readCtx, view.Values)

				require.NoError(t, result.Err, "read cancellation stays quiet")
				for pane := range view.Values.All() {
					require.Equal(t, status.Unknown, result.Values.At(pane, time.Now()))
				}
				require.Equal(t, tc.wantCalls, calls)
				return list.ItemID{}, nil
			}

			require.NoError(t, service.Run(t.Context()))
		})
	}
}
