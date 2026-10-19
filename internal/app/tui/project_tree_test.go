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
		{ID: "%2", Path: "/agent", Program: "opencode", Command: "node"},
		{ID: "%1", Active: true, Path: "/shell", Command: "zsh"},
	}
	state.Sessions[0].DefaultShell = "/bin/zsh"
	b := &projectionStatusBackend{stubBackend: stubBackend{state: state}, server: server}
	adapter := newLiveAdapter(func() (Backend, error) { return b, nil }, nil, nil)
	adapter.projection = projectionTree
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		active:   {State: status.Idle, ValidUntil: now.Add(time.Minute)},
		inactive: {State: status.Blocked, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)

	snapshot, err := adapter.Load(t.Context())

	require.NoError(t, err)
	require.Empty(t, snapshot.Items[0].Children[0].Trailing)
	require.Equal(t, "/shell", snapshot.Items[0].Children[0].Secondary)
	var update ui.StatusUpdate
	adapter.bindStatusUpdate(daemon.View{Values: values, Locations: []daemon.Location{
		{Session: "$1", Window: "@1", Pane: "%1"}, {Session: "$1", Window: "@1", Pane: "%2"},
	}}, &update)
	id := windowItemID("$1", "@1")
	panes := update.Windows[id]
	require.Len(t, panes, 2)
	require.Equal(t, inactive, panes[0].Ref)
	require.Equal(t, "opencode", panes[0].Details.Trailing)
	require.Equal(t, "/agent", panes[0].Details.Secondary)
	require.Equal(t, active, panes[1].Ref)
	require.True(t, panes[1].Active)
	require.Empty(t, panes[1].Details.Trailing)

	model, err := list.New(snapshot)
	require.NoError(t, err)
	model.SetDetails(map[list.ItemID]list.Details{id: panes[0].Details})
	model.SetQuery("opencode")
	require.Len(t, model.Rows(), 2)
	require.Equal(t, id, model.Rows()[1].Item.ID)

	model.SetQuery("/shell")
	require.Empty(t, model.Rows(), "search must use the displayed pane's path")
	item, err := adapter.resolveItem(id)
	require.NoError(t, err)
	require.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1"}, item.Target)
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
