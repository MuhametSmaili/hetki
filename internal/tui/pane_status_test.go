package tui

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/tui/list"
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
				bindings["window"] = append(bindings["window"], pane)
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

			got := bindings.StateAt("window", snapshot, now)

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
	m.paneStatus.options.Bindings = StatusBindings{"window": {pane}}
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

func TestStatusFramesStayLocalAndCoalesced(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	m := paneStatusModel(t, status.Working, now.Add(time.Minute))
	reads := 0
	m.paneStatus.options.Read = func(context.Context, status.Snapshot) StatusUpdate {
		reads++
		return StatusUpdate{}
	}
	m, scheduled := m.syncStatusFrame(now)

	_, duplicate := m.syncStatusFrame(now)
	next, effect := m.update(statusFrameMsg{generation: m.paneStatus.frameGeneration})

	require.NotNil(t, scheduled)
	require.Nil(t, duplicate)
	require.Nil(t, effect, "a frame must not schedule a status read")
	require.Zero(t, reads)
	require.True(t, next.(model).paneStatus.frameDue.IsZero())
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
	var deadline time.Time
	var hasDeadline bool
	m.paneStatus.options.Read = func(ctx context.Context, previous status.Snapshot) StatusUpdate {
		deadline, hasDeadline = ctx.Deadline()
		reads++
		return StatusUpdate{Values: previous}
	}

	next, cmd := m.readPaneStatus()
	reading := next.(model)
	_, duplicate := reading.readPaneStatus()
	result := cmd().(statusResultMsg)
	finished, poll := reading.handleStatusResult(result)

	require.True(t, reading.paneStatus.reading)
	require.Nil(t, duplicate)
	require.Equal(t, 1, reads)
	require.True(t, hasDeadline)
	require.LessOrEqual(t, time.Until(deadline), statusReadTimeout)
	require.False(t, finished.(model).paneStatus.reading)
	require.NotNil(t, poll)
}

func TestStatusPollingStopsOnQuit(t *testing.T) {
	m := paneStatusModel(t, status.Working, time.Now().Add(time.Minute))
	m.paneStatus.options.Read = func(_ context.Context, previous status.Snapshot) StatusUpdate { return StatusUpdate{Values: previous} }
	m.quitting = true

	_, read := m.readPaneStatus()
	_, poll := m.handleStatusResult(statusResultMsg{values: m.paneStatus.values})

	require.Nil(t, read)
	require.Nil(t, poll)
}

func TestWatchReadBudgetAndRetryCadence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		watching bool
		err      error
		delay    time.Duration
	}{
		{name: "watch rearms immediately", watching: true},
		{name: "older daemon keeps polling", delay: statusPollInterval},
		{name: "watch failure backs off", watching: true, err: errors.New("offline"), delay: statusPollInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m := newModel(viewSnapshot(), nil)
				m.paneStatus.options.ReadTimeout = 23 * time.Second
				m.paneStatus.options.Read = func(ctx context.Context, previous status.Snapshot) StatusUpdate {
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					require.Equal(t, 23*time.Second, time.Until(deadline))
					return StatusUpdate{Values: previous, Watching: tc.watching, Err: tc.err}
				}

				next, read := m.readPaneStatus()
				_, retry := next.(model).handleStatusResult(read().(statusResultMsg))
				start := time.Now()
				message := retry()

				require.IsType(t, statusPollMsg{}, message)
				require.Equal(t, tc.delay, time.Since(start))
			})
		})
	}
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
			pending := statusResultMsg{values: m.paneStatus.values, bindings: m.paneStatus.options.Bindings}
			pane := m.paneStatus.options.Bindings["window"][0]
			pane.Generation = test.generation
			flat := list.Snapshot{Items: []list.Item{{ID: "pane", Primary: "pane"}}}
			replacement := actionResultMsg{result: ActionResult{
				Snapshot: &flat, StatusBindings: StatusBindings{"pane": {pane}},
			}}

			rebound, cmd := m.handleActionResult(replacement)
			completed, _ := rebound.(model).handleStatusResult(pending)
			current := completed.(model)

			require.Nil(t, cmd)
			require.Equal(t, test.want, current.paneStatus.options.Bindings.StateAt("pane", current.paneStatus.values, now))
		})
	}
}

func TestStatusPollReplacesBindingsAndClearsReadFailure(t *testing.T) {
	m := paneStatusModel(t, status.Working, time.Now().Add(time.Minute))
	pane := m.paneStatus.options.Bindings["window"][0]
	failed, _ := m.handleStatusResult(statusResultMsg{values: m.paneStatus.values.Invalidate(), err: errors.New("status unavailable")})
	m = failed.(model)
	require.Equal(t, status.Unknown, m.paneStatus.options.Bindings.StateAt("window", m.paneStatus.values, time.Now()))
	require.ErrorContains(t, m.paneStatus.readErr, "status unavailable")
	pane.Generation = "new daemon generation"
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: {State: status.Idle, ValidUntil: time.Now().Add(time.Minute)}})
	require.NoError(t, err)

	recovered, _ := m.handleStatusResult(statusResultMsg{values: values, bindings: StatusBindings{"window": {pane}}})
	m = recovered.(model)

	require.NoError(t, m.paneStatus.readErr)
	require.Equal(t, status.Idle, m.paneStatus.options.Bindings.StateAt("window", m.paneStatus.values, time.Now()))
	cleared, _ := m.handleStatusResult(statusResultMsg{bindings: StatusBindings{}})
	require.Empty(t, cleared.(model).paneStatus.options.Bindings)
}
