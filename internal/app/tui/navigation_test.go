package tui

import (
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/frecency"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestTreeOpenUsesRequestedPaneAndValidatesMembership(t *testing.T) {
	for _, tc := range []struct {
		name    string
		removed bool
		moved   bool
	}{
		{name: "present"},
		{name: "removed", removed: true},
		{name: "moved to another window", removed: true, moved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubBackend{state: liveState()}
			pane := backend.Pane{ID: "%2", Path: "/agent", Command: "pi"}
			stub.state.Sessions[0].Windows[0].Panes = append(stub.state.Sessions[0].Windows[0].Panes, pane)
			store := frecency.NewStore(filepath.Join(t.TempDir(), "frecency.json"))
			adapter := loadedAdapter(t, stub)
			adapter.frecency = store
			if tc.removed {
				stub.state.Sessions[0].Windows[0].Panes = stub.state.Sessions[0].Windows[0].Panes[:1]
			}
			if tc.moved {
				stub.state.Sessions[0].Windows = append(stub.state.Sessions[0].Windows,
					backend.Window{ID: "@2", Panes: []backend.Pane{pane}})
			}

			opened, err := adapter.Execute(t.Context(), ui.ActionRequest{
				ActionID: ui.ActionOpen, ItemID: windowItemID("$1", "@1"), PaneID: "%2",
			})

			if tc.removed {
				require.ErrorContains(t, err, "stale")
				require.True(t, opened.Navigation.IsZero())
				require.Empty(t, stub.switchCalls)
				return
			}
			require.NoError(t, err)
			require.NoError(t, adapter.Navigate(t.Context(), opened.Navigation))
			require.Equal(t, []backend.Target{{SessionID: "$1", WindowID: "@1", PaneID: "%2"}}, stub.switchCalls)
			records, err := store.Load()
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, "/agent", records[0].Path)
		})
	}
}

func TestTreeOpenKeepsTheDisplayedPaneWithoutStatus(t *testing.T) {
	store := frecency.NewStore(filepath.Join(t.TempDir(), "frecency.json"))
	stub := &stubBackend{state: liveState()}
	stub.state.Sessions[0].Windows[0].Panes = append(stub.state.Sessions[0].Windows[0].Panes,
		backend.Pane{ID: "%2", Path: "/other", Command: "pi"})
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, store, nil)
	adapter.projection = projectionTree
	snapshot, err := adapter.Load(t.Context())
	require.NoError(t, err)
	row := snapshot.Items[0].Children[0]
	require.Equal(t, "/work/editor", row.Secondary)
	stub.state.Sessions[0].Windows[0].Panes[0].Active = false
	stub.state.Sessions[0].Windows[0].Panes[1].Active = true

	opened, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: row.ID})
	require.NoError(t, err)
	_, err = adapter.Load(t.Context())
	require.NoError(t, err)
	delete(adapter.index, row.ID)
	err = adapter.Navigate(t.Context(), opened.Navigation)

	require.NoError(t, err)
	require.Equal(t, []backend.Target{{SessionID: "$1", WindowID: "@1", PaneID: "%1"}}, stub.switchCalls)
	records, err := store.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "/work/editor", records[0].Path)
}
