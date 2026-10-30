package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/frecency"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/assert"
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
	for _, tt := range []struct {
		view    ui.ViewMode
		project func(backend.StateResult, string) (list.Snapshot, itemIndex, error)
	}{
		{view: ui.ViewFlat, project: projectFlat},
		{view: ui.ViewTree, project: projectTree},
	} {
		t.Run(string(tt.view), func(t *testing.T) {
			state.Sessions[1].Last = false
			plain, plainIndex, err := tt.project(state, "")
			require.NoError(t, err)
			state.Sessions[1].Last = true
			marked, index, err := tt.project(state, "")
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
			ctx := t.Context()
			_, err = adapter.Load(ctx, tt.view)
			require.NoError(t, err)
			ordinary, err := adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: lastID})
			require.NoError(t, err)
			require.Equal(t, 2, stub.queryCalls)
			record := *adapter.pendingNavigation
			shortcut, err := adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionLastSession})
			require.NoError(t, err)
			require.Equal(t, ordinary.Navigation, shortcut.Navigation)
			require.Equal(t, ordinary.Message, shortcut.Message)
			require.NotNil(t, shortcut.Snapshot)
			require.Equal(t, marked, *shortcut.Snapshot)
			require.Equal(t, record.path, adapter.pendingNavigation.path)
			require.Equal(t, record.session, adapter.pendingNavigation.session)
			require.Equal(t, 3, stub.queryCalls, "shortcut must query history once without a second validation query")
			require.Empty(t, stub.switchCalls, "opening must not switch before terminal restoration")
			require.NoError(t, adapter.Navigate(ctx, shortcut.Navigation))
			require.Equal(t, []backend.Target{index[lastID].Target}, stub.switchCalls)
			require.Equal(t, 3, stub.queryCalls)

			stub.state = backend.StateResult{}
			shortcut, err = adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionLastSession})
			require.ErrorContains(t, err, "no previous session available")
			require.Empty(t, shortcut.Navigation)
			require.Nil(t, adapter.pendingNavigation)
			require.Equal(t, 4, stub.queryCalls)
			stub.state = liveState()
			_, err = adapter.Load(ctx, ui.ViewTree)
			require.NoError(t, err)
			shortcut, err = adapter.Execute(ctx, ui.ActionRequest{ActionID: ui.ActionLastSession})
			require.ErrorContains(t, err, "no previous session available")
			require.Empty(t, shortcut.Navigation)
			require.Equal(t, 6, stub.queryCalls, "history must be queried even when the loaded view has no marker")
		})
	}
}

func TestLastSessionQueryFailurePreservesProjectionAndClearsPreparedNavigation(t *testing.T) {
	stub := &stubBackend{state: historyState()}
	adapter := loadedAdapter(t, stub)
	opened, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: sessionItemID("$3")})
	require.NoError(t, err)
	input := adapter.input
	stub.stateErr = assert.AnError

	result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionLastSession})

	require.ErrorIs(t, err, assert.AnError)
	require.True(t, result.Navigation.IsZero())
	require.Nil(t, result.Snapshot)
	require.Same(t, input, adapter.input)
	require.Nil(t, adapter.pendingNavigation)
	require.Error(t, adapter.Navigate(t.Context(), opened.Navigation))
	require.Empty(t, stub.switchCalls)
}

func TestLastSessionKeepsLoadedFrecencyRanking(t *testing.T) {
	store := frecency.NewStore(filepath.Join(t.TempDir(), "frecency.json"))
	require.NoError(t, store.Record(t.Context(), "/visited", "visited"))
	stub := &stubBackend{state: historyState()}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, store, nil)
	snapshot, err := adapter.Load(t.Context(), ui.ViewFlat)
	require.NoError(t, err)
	require.Equal(t, destinationItemID("$3", "@3", "%3"), snapshot.Items[0].ID)
	require.NoError(t, store.Record(t.Context(), "/previous", "previous"))
	require.NoError(t, store.Record(t.Context(), "/previous", "previous"))

	result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionLastSession})

	require.NoError(t, err)
	require.NotNil(t, result.Snapshot)
	require.Equal(t, snapshot.Items[0].ID, result.Snapshot.Items[0].ID)
	requireHistoryMarker(t, *result.Snapshot, destinationItemID("$2", "@2", "%2"))
	require.Equal(t, 2, stub.queryCalls)
}
