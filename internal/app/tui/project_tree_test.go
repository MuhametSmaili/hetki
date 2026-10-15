package tui

import (
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestTreeShowsActivePaneProgramButAggregatesEveryPaneStatus(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	active := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	inactive := status.PaneRef{Server: "server", ID: "%2", Generation: "birth"}
	state := liveState()
	state.Sessions[0].Windows[0].Panes = []backend.Pane{
		{ID: "%2", Program: "opencode", Command: "node", StatusRef: inactive},
		{ID: "%1", Active: true, Program: "pi", Command: "node", StatusRef: active},
	}
	adapter := testAdapter(&stubBackend{state: state})
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		active:   {State: status.Idle, ValidUntil: now.Add(time.Minute)},
		inactive: {State: status.Blocked, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)

	snapshot, err := adapter.Load(t.Context())

	require.NoError(t, err)
	require.Equal(t, "pi", snapshot.Items[0].Children[0].Trailing)
	require.Equal(t, status.Blocked, adapter.StatusBindings().StateAt(windowItemID("$1", "@1"), values, now))

	model, err := list.New(snapshot)
	require.NoError(t, err)
	model.SetQuery("pi")
	require.Len(t, model.Rows(), 2)
	require.Equal(t, windowItemID("$1", "@1"), model.Rows()[1].Item.ID)

	model.SetQuery("opencode")
	require.Empty(t, model.Rows(), "an inactive pane's program is not displayed or searched")
}

func TestTreeDoesNotInventAnActivePaneProgram(t *testing.T) {
	state := liveState()
	state.Sessions[0].Windows[0].Panes[0].Active = false
	state.Sessions[0].Windows[0].Panes[0].Program = "pi"
	adapter := testAdapter(&stubBackend{state: state})

	snapshot, err := adapter.Load(t.Context())

	require.NoError(t, err)
	require.Empty(t, snapshot.Items[0].Children[0].Trailing)
}
