package tui

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestWindowStatusPriorityAndFreshness(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		states []status.State
		stale  bool
		want   status.State
	}{
		{"no evidence", nil, false, status.Unreported},
		{"cleared", []status.State{status.Withdrawn}, false, status.Unreported},
		{"idle", []status.State{status.Unreported, status.Withdrawn, status.Idle}, false, status.Idle},
		{"unknown before idle", []status.State{status.Idle, status.Unknown}, false, status.Unknown},
		{"working before unknown", []status.State{status.Unknown, status.Working, status.Idle}, false, status.Working},
		{"blocked before working", []status.State{status.Working, status.Blocked, status.Unknown}, false, status.Blocked},
		{"blocked first", []status.State{status.Blocked, status.Working, status.Idle}, false, status.Blocked},
		{"expired blocked", []status.State{status.Blocked, status.Idle}, true, status.Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			bindings := StatusBindings{}
			values := make(map[status.PaneRef]status.Value)
			for i, state := range test.states {
				pane := status.PaneRef{Server: "server", ID: string(rune('a' + i)), Generation: "birth"}
				bindings[list.ItemID{ID: "window"}] = append(bindings[list.ItemID{ID: "window"}], pane)
				if state == status.Unreported {
					continue
				}
				deadline := now.Add(time.Minute)
				if test.stale && i == 0 {
					deadline = now
				}
				values[pane] = status.Value{State: state, ValidUntil: deadline}
			}
			snapshot, err := status.NewSnapshot(values)
			require.NoError(t, err)

			got := bindings.StateAt(list.ItemID{ID: "window"}, snapshot, now)

			require.Equal(t, test.want, got)
		})
	}
}

func paneStatusModel(t *testing.T, state status.State, deadline time.Time) model {
	t.Helper()
	m := newModel(viewSnapshot(), nil)
	pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: {State: state, ValidUntil: deadline}})
	require.NoError(t, err)
	m.paneStatus.values = values
	m.paneStatus.options.Bindings = StatusBindings{{ID: "window"}: {pane}}
	return m
}

func TestStatusFrameScheduling(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		state     status.State
		remaining time.Duration
		collapsed bool
		want      time.Time
	}{
		{"working animates", status.Working, time.Minute, false, now.Add(100 * time.Millisecond)},
		{"blocked waits for expiry", status.Blocked, time.Minute, false, now.Add(time.Minute)},
		{"idle waits for expiry", status.Idle, time.Minute, false, now.Add(time.Minute)},
		{"unknown stays still", status.Unknown, time.Minute, false, time.Time{}},
		{"withdrawn stays still", status.Withdrawn, time.Minute, false, time.Time{}},
		{"hidden window stays still", status.Working, time.Minute, true, time.Time{}},
		{"expiry precedes animation", status.Working, 20 * time.Millisecond, false, now.Add(20 * time.Millisecond)},
		{"expired status stops animating", status.Working, 0, false, time.Time{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := paneStatusModel(t, test.state, now.Add(test.remaining))
			if test.collapsed {
				m.items.CollapseAll()
			}

			due := m.nextStatusFrame(now)

			require.Equal(t, test.want, due)
		})
	}
}

func TestStatusFramesExpireWhileReadIsBlocked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deadline := time.Now().Add(5 * time.Second)
		m := paneStatusModel(t, status.Blocked, deadline)
		ctx, cancel := context.WithCancel(t.Context())
		m.ctx = ctx
		entered := make(chan struct{})
		done := make(chan struct{})
		reads := 0
		m.paneStatus.options.Read = func(ctx context.Context, _ uint64) StatusUpdate {
			reads++
			close(entered)
			<-ctx.Done()
			return StatusUpdate{Err: ctx.Err()}
		}
		next, read := m.readPaneStatus()
		m = next.(model)
		go func() {
			read()
			close(done)
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("status read did not stop")
			}
		})
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("status read did not start")
		}
		selected := selectedNodeID(m)
		m, scheduled := m.syncStatusFrame(time.Now())
		require.NotNil(t, scheduled)
		_, duplicate := m.syncStatusFrame(time.Now())
		require.Nil(t, duplicate)

		next, effect := m.Update(scheduled())
		m = next.(model)

		require.Equal(t, deadline, time.Now())
		require.Equal(t, status.Unknown, m.paneStatus.rowValue(list.ItemID{ID: "window"}, time.Now()).State)
		require.Equal(t, selected, selectedNodeID(m))
		require.True(t, m.paneStatus.reading)
		require.Equal(t, 1, reads)
		require.True(t, m.paneStatus.frameDue.IsZero())
		require.Nil(t, effect, "expiry must not schedule another read")
		select {
		case <-done:
			t.Fatal("expiry completed the blocked read")
		default:
		}
	})
}

func TestStatusPollingWithoutProvider(t *testing.T) {
	m := newModel(viewSnapshot(), nil)

	initial := m.Init()
	_, poll := m.readPaneStatus()

	require.Nil(t, initial)
	require.Nil(t, poll)
}

func TestStatusPollingIsSingleFlight(t *testing.T) {
	m := newModel(viewSnapshot(), nil)
	reads := 0
	m.paneStatus.options.Read = func(context.Context, uint64) StatusUpdate {
		reads++
		return StatusUpdate{Sequence: 1}
	}

	next, cmd := m.readPaneStatus()
	reading := next.(model)
	_, duplicate := reading.readPaneStatus()
	finished, poll := reading.handleStatusResult(cmd().(statusResultMsg))

	require.True(t, reading.paneStatus.reading)
	require.Nil(t, duplicate)
	require.Equal(t, 1, reads)
	require.False(t, finished.(model).paneStatus.reading)
	require.EqualValues(t, 1, finished.(model).paneStatus.sequence)
	require.NotNil(t, poll)
}

func TestStatusPollingStopsOnQuit(t *testing.T) {
	m := paneStatusModel(t, status.Working, time.Now().Add(time.Minute))
	m.paneStatus.options.Read = func(context.Context, uint64) StatusUpdate { return StatusUpdate{} }
	m.quitting = true

	_, read := m.readPaneStatus()
	_, poll := m.handleStatusResult(statusResultMsg{update: StatusUpdate{Values: m.paneStatus.values}})

	require.Nil(t, read)
	require.Nil(t, poll)
}

func TestStatusReadCadence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		delay time.Duration
	}{
		{name: "success reads the next change at once"},
		{name: "failure waits before the next read", err: errors.New("offline"), delay: statusPollInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m := newModel(viewSnapshot(), nil)
				var after []uint64
				m.paneStatus.options.Read = func(_ context.Context, sequence uint64) StatusUpdate {
					after = append(after, sequence)
					return StatusUpdate{Err: tc.err, Sequence: 4}
				}

				next, read := m.readPaneStatus()
				applied, retry := next.(model).handleStatusResult(read().(statusResultMsg))
				start := time.Now()
				message := retry()
				_, again := applied.(model).readPaneStatus()
				again()

				require.IsType(t, statusPollMsg{}, message)
				require.Equal(t, tc.delay, time.Since(start))
				require.Equal(t, []uint64{0, 4}, after, "the next read waits for a newer result")
			})
		})
	}
}

func TestDiscardedStatusResultIsReadAgain(t *testing.T) {
	m := paneStatusModel(t, status.Working, time.Now().Add(time.Minute))
	m.paneStatus.sequence = 3
	m.busy = true

	ignored, _ := m.handleStatusResult(statusResultMsg{update: StatusUpdate{Sequence: 4}})

	require.EqualValues(t, 3, ignored.(model).paneStatus.sequence, "a discarded result keeps the old sequence")
}

func TestProjectionReplacementBindsStatusByIncarnation(t *testing.T) {
	for _, test := range []struct {
		name       string
		generation string
		want       status.State
	}{
		{"same pane in flat view", "birth", status.Working},
		{"replacement pane rejects old result", "new birth", status.Unreported},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			m := paneStatusModel(t, status.Working, now.Add(time.Minute))
			pending := statusResultMsg{update: StatusUpdate{
				Values: m.paneStatus.values, Bindings: m.paneStatus.options.Bindings, Sequence: 1,
			}}
			pane := m.paneStatus.options.Bindings[list.ItemID{ID: "window"}][0]
			pane.Generation = test.generation
			flat := list.Snapshot{Items: []list.Item{{ID: list.ItemID{ID: "pane"}, Primary: "pane"}}}
			replacement := actionResultMsg{result: ActionResult{
				Snapshot: &flat, Status: &StatusUpdate{
					Values: m.paneStatus.values, Bindings: StatusBindings{{ID: "pane"}: {pane}}, Sequence: 2,
				},
			}}

			m.busy = true
			ignored, _ := m.handleStatusResult(pending)
			rebound, cmd := ignored.(model).handleActionResult(replacement)
			completed, _ := rebound.(model).handleStatusResult(pending)
			current := completed.(model)

			require.Nil(t, cmd)
			require.EqualValues(t, 2, current.paneStatus.sequence, "only the replacement was applied")
			require.Equal(t, test.want, current.paneStatus.options.Bindings.StateAt(list.ItemID{ID: "pane"}, current.paneStatus.values, now))
		})
	}
}

func TestStatusPollReplacesBindingsAndClearsReadFailure(t *testing.T) {
	m := paneStatusModel(t, status.Working, time.Now().Add(time.Minute))
	pane := m.paneStatus.options.Bindings[list.ItemID{ID: "window"}][0]
	failed, _ := m.handleStatusResult(statusResultMsg{update: StatusUpdate{Values: m.paneStatus.values.Invalidate(), Err: errors.New("status unavailable")}})
	m = failed.(model)
	require.Equal(t, status.Unknown, m.paneStatus.options.Bindings.StateAt(list.ItemID{ID: "window"}, m.paneStatus.values, time.Now()))
	require.ErrorContains(t, m.paneStatus.readErr, "status unavailable")
	pane.Generation = "new daemon generation"
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: {State: status.Idle, ValidUntil: time.Now().Add(time.Minute)}})
	require.NoError(t, err)

	recovered, _ := m.handleStatusResult(statusResultMsg{update: StatusUpdate{Values: values, Bindings: StatusBindings{{ID: "window"}: {pane}}}})
	m = recovered.(model)

	require.NoError(t, m.paneStatus.readErr)
	require.Equal(t, status.Idle, m.paneStatus.options.Bindings.StateAt(list.ItemID{ID: "window"}, m.paneStatus.values, time.Now()))
	cleared, _ := m.handleStatusResult(statusResultMsg{update: StatusUpdate{Bindings: StatusBindings{}}})
	require.Empty(t, cleared.(model).paneStatus.options.Bindings)
}
