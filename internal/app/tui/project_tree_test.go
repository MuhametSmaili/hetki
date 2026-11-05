package tui

import (
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestTreeBindsPaneDetailsToTheirOwnStatus(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	server := observe.Server{PID: 123, Birth: "fixture", Started: 1}
	active := status.PaneRef{Server: server.Identity(), ID: "%1", Generation: "birth"}
	inactive := status.PaneRef{Server: server.Identity(), ID: "%2", Generation: "birth"}
	state := liveState()
	state.Sessions[0].Windows[0].Panes = []backend.Pane{
		{ID: "%2", Index: 1, Path: "/agent", Program: "opencode", Command: "node"},
		{ID: "%1", Active: true, Path: "/shell", Command: "zsh"},
	}
	state.Sessions[0].DefaultShell = "/bin/zsh"
	b := &projectionStatusBackend{state: state, server: server}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)

	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		active:   {State: status.Idle, ValidUntil: now.Add(time.Minute)},
		inactive: {State: status.Blocked, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)

	snapshot, err := adapter.Load(t.Context(), ui.ViewTree)

	require.NoError(t, err)
	window := snapshot.Items[0].Children[0]
	require.Empty(t, window.Trailing)
	require.Empty(t, window.Secondary)
	require.Len(t, window.Children, 2)
	agent, shell := window.Children[0], window.Children[1]
	require.Equal(t, "1", agent.Primary)
	require.Equal(t, "opencode", agent.Trailing)
	require.Equal(t, "/agent", agent.Secondary)
	require.Equal(t, "0", shell.Primary)
	require.Empty(t, shell.Trailing)
	require.Equal(t, "/shell", shell.Secondary)
	require.Equal(t, shell.ID, snapshot.ActiveItemID)
	var update ui.StatusUpdate
	adapter.bindStatusUpdate(daemon.View{Values: values, Locations: []daemon.Location{
		{Session: "$1", Window: "@1", Pane: "%1"}, {Session: "$1", Window: "@1", Pane: "%2"},
	}}, &update)
	require.Empty(t, update.Windows)
	require.NotContains(t, update.Bindings, window.ID)
	require.Equal(t, status.Blocked, update.Bindings.StateAt(agent.ID, values, now))
	require.Equal(t, status.Idle, update.Bindings.StateAt(shell.ID, values, now))

	model, err := list.New(snapshot)
	require.NoError(t, err)
	model.SetQuery("opencode")
	require.Len(t, model.Rows(), 3)
	require.Equal(t, window.ID, model.Rows()[1].Item.ID)
	require.Equal(t, agent.ID, model.Rows()[2].Item.ID)

	model.SetQuery("/shell")
	require.Len(t, model.Rows(), 3)
	require.Equal(t, shell.ID, model.Rows()[2].Item.ID)
	item, err := adapter.resolveItem(agent.ID)
	require.NoError(t, err)
	require.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%2"}, item.Target)
	model.SetFilter("", map[list.ItemID]struct{}{agent.ID: {}})
	require.Len(t, model.Rows(), 3, "tracked panes retain ancestors, not untracked siblings")
	require.Equal(t, agent.ID, model.Rows()[2].Item.ID)

	adapter.bindStatusUpdate(daemon.View{}, &update)

	require.Empty(t, update.Bindings)
	item, err = adapter.resolveItem(shell.ID)
	require.NoError(t, err)
	require.Equal(t, "%1", item.Target.PaneID, "untracked panes still have an exact navigation target")
}

func TestTreeDoesNotInventAnActivePaneProgram(t *testing.T) {
	state := liveState()
	state.Sessions[0].Windows[0].Panes[0].Active = false
	state.Sessions[0].Windows[0].Panes[0].Program = "pi"
	adapter := testAdapter(&stubBackend{state: state})

	snapshot, err := adapter.Load(t.Context(), ui.ViewTree)

	require.NoError(t, err)
	require.Empty(t, snapshot.Items[0].Children[0].Trailing)
}

func TestTreeKeepsSinglePaneWindowsCompact(t *testing.T) {
	state := liveState()
	state.Sessions[0].Windows[0].Panes[0].Program = "nvim"

	snapshot, index, err := projectTree(state, "")

	require.NoError(t, err)
	window := snapshot.Items[0].Children[0]
	require.Empty(t, window.Children)
	require.Equal(t, "/work/editor", window.Secondary)
	require.Equal(t, "nvim", window.Trailing)
	require.False(t, index[window.ID].PaneRows)
	require.Equal(t, window.ID, snapshot.ActiveItemID)
}

func TestTreePaneSelectionSurvivesProjectionSwitches(t *testing.T) {
	stub := &stubBackend{state: linkedPaneState()}
	adapter := loadedAdapter(t, stub)
	id := destinationItemID("$1", "@1", "%2")

	for _, view := range []ui.ViewMode{ui.ViewFlat, ui.ViewTree} {
		result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection, ItemID: id})

		require.NoError(t, err)
		require.Equal(t, view, result.View)
		require.Equal(t, id, result.SelectItemID)
	}
	require.Equal(t, 1, stub.queryCalls)
}
