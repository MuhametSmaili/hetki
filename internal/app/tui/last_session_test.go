package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestLastSessionMarkerAndShortcutUseTheSameDestination(t *testing.T) {
	state := liveState()
	state.Sessions = append(state.Sessions, backend.Session{
		ID: "$2", Name: "previous", Windows: []backend.Window{
			{ID: "@2", Name: "editor", Active: true, Panes: []backend.Pane{
				{ID: "%2", Path: "/shared", Active: true},
				{ID: "%3", Path: "/shared"},
				{ID: "%4", Path: "/other"},
			}},
			{ID: "@3", Name: "shell", Panes: []backend.Pane{{ID: "%5", Path: "/shared", Active: true}}},
		},
	})
	for _, project := range []func(backend.StateResult, string) (list.Snapshot, itemIndex, error){projectFlat, projectTree} {
		state.Sessions[1].Last = false
		plain, plainIndex, err := project(state, "")
		require.NoError(t, err)
		state.Sessions[1].Last = true
		marked, index, err := project(state, "")
		require.NoError(t, err)
		baseline, err := list.New(plain)
		require.NoError(t, err)
		model, err := list.New(marked)
		require.NoError(t, err)
		require.Equal(t, baseline.ShownRoots(), model.ShownRoots())
		require.Equal(t, baseline.Snapshot().ActiveItemID, model.Snapshot().ActiveItemID)
		count := 0
		var lastID list.ItemID
		for i, row := range model.Rows() {
			want := baseline.Rows()[i]
			require.Equal(t, want.Item.ID, row.Item.ID, "ordering must not change")
			require.Equal(t, want.Item.SearchFields, row.Item.SearchFields)
			item := index[row.Item.ID]
			require.Equal(t, strings.HasSuffix(row.Item.Primary, " ↶"), item.Last)
			item.Last = false
			require.Equal(t, plainIndex[item.ID], item, "markers must not change existing item data")
			if strings.HasSuffix(row.Item.Primary, " ↶") {
				lastID = row.Item.ID
				count++
				require.Equal(t, windowItemID("$2", "@2"), index[row.Item.ID].windowRowID())
				require.Equal(t, want.Item.Primary+" ↶", row.Item.Primary)
			} else {
				require.Equal(t, want.Item.Primary, row.Item.Primary)
			}
		}
		require.Equal(t, 1, count)
		model.SetQuery("previous")
		require.NotEmpty(t, model.Rows())
		model.SetQuery("↶")
		require.Empty(t, model.Rows(), "the marker is not a search field")
		model.SetQuery("core")
		for _, row := range model.Rows() {
			require.NotContains(t, row.Item.Primary, "↶", "filtering hides the marked row normally")
		}

		stub := &stubBackend{state: state}
		adapter := testAdapter(stub)
		adapter.index = index
		ctx := context.Background()
		ordinary, err := adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: lastID})
		require.NoError(t, err)
		require.Equal(t, 1, stub.queryCalls)
		record := *adapter.pendingRecord
		shortcut, err := adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionLastSession})
		require.NoError(t, err)
		require.Equal(t, ordinary, shortcut)
		require.Equal(t, record, *adapter.pendingRecord)
		require.Equal(t, 2, stub.queryCalls, "shortcut must use only ordinary open validation")
		require.Empty(t, stub.switchCalls, "opening must not switch before terminal restoration")
		require.NoError(t, adapter.Navigate(ctx, shortcut.Navigation))
		require.Equal(t, []backend.Target{index[lastID].Target}, stub.switchCalls)
		require.Equal(t, 2, stub.queryCalls)

		stub.state = backend.StateResult{}
		shortcut, err = adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionLastSession})
		require.ErrorContains(t, err, "stale")
		require.Empty(t, shortcut.Navigation)
		require.Nil(t, adapter.pendingRecord)
		require.Equal(t, 3, stub.queryCalls)
		stub.state = liveState()
		_, err = adapter.Load(ctx)
		require.NoError(t, err)
		shortcut, err = adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionLastSession})
		require.ErrorContains(t, err, "no previous session available")
		require.Empty(t, shortcut.Navigation)
		require.Equal(t, 4, stub.queryCalls, "no marker means no shortcut IPC")
	}
}
