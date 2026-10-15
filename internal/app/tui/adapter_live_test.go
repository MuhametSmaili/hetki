package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/frecency"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubBackend struct {
	state       backend.StateResult
	stateErr    error
	queryCalls  int
	applyErr    error
	applyCalls  [][]backend.Action
	applyHook   func([]backend.Action)
	switchCalls []backend.Target
	switchErr   error
	switchHook  func(backend.Target)
	captureHook func(context.Context, backend.Target) (string, error)
}

func (s *stubBackend) CapturePane(ctx context.Context, target backend.Target) (string, error) {
	if s.captureHook == nil {
		panic("unexpected pane capture")
	}
	return s.captureHook(ctx, target)
}

func (s *stubBackend) QueryState(context.Context) (backend.StateResult, error) {
	s.queryCalls++
	if s.stateErr != nil {
		return backend.StateResult{}, s.stateErr
	}
	return s.state, nil
}

func (s *stubBackend) Apply(_ context.Context, actions []backend.Action) error {
	cloned := append([]backend.Action(nil), actions...)
	s.applyCalls = append(s.applyCalls, cloned)
	if s.applyHook != nil {
		s.applyHook(cloned)
	}
	return s.applyErr
}

func (s *stubBackend) Switch(_ context.Context, target backend.Target) error {
	s.switchCalls = append(s.switchCalls, target)
	if s.switchHook != nil {
		s.switchHook(target)
	}
	return s.switchErr
}

func liveState() backend.StateResult {
	return backend.StateResult{
		Active: backend.ActiveContext{SessionID: "$1", Session: "core", WindowID: "@1", WindowIndex: 1, PaneID: "%1", Pane: 0, Path: "/work/editor"},
		Sessions: []backend.Session{{
			ID: "$1", Name: "core", WorkspacePath: "/work/.hetki.yaml",
			Windows: []backend.Window{{ID: "@1", Index: 1, Name: "editor", Path: "/work/editor", Active: true, Panes: []backend.Pane{{ID: "%1", Index: 0, Path: "/work/editor", Active: true}}}},
		}},
	}
}

func testAdapter(stub *stubBackend) *LiveAdapter {
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, nil, nil)
	adapter.projection = projectionTree
	return adapter
}

func loadedAdapter(t *testing.T, stub *stubBackend) *LiveAdapter {
	t.Helper()
	adapter := testAdapter(stub)
	_, err := adapter.Load(context.Background())
	require.NoError(t, err)
	return adapter
}

func text(value string) *string { return &value }

func TestLiveAdapterProjectsPresentationAndKeepsTargetsInOwnerIndex(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := testAdapter(stub)

	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	require.NoError(t, validateProjection(snapshot, adapter.index))
	require.Len(t, snapshot.Items, 1)
	require.Len(t, snapshot.Items[0].Children, 1)
	assert.Equal(t, sessionItemID("$1"), snapshot.Items[0].ID)
	assert.Equal(t, windowItemID("$1", "@1"), snapshot.Items[0].Children[0].ID)
	assert.Equal(t, windowItemID("$1", "@1"), snapshot.ActiveItemID)
	assert.Equal(t, "core", snapshot.Items[0].Primary)
	assert.Equal(t, "1 editor", snapshot.Items[0].Children[0].Primary)
	assert.Empty(t, snapshot.Items[0].Children[0].Children)
	assert.Equal(t, backend.Target{SessionID: "$1", WindowID: "@1"}, adapter.index[windowItemID("$1", "@1")].Target)
}

func TestLiveAdapterStartsWithTheFlatProjection(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, nil, nil)

	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 1)
	assert.Empty(t, snapshot.Items[0].Children)
	assert.Equal(t, destinationItemID("$1", "@1", "%1"), snapshot.ActiveItemID)
}

func TestLiveAdapterKeepsLoadedFrecencyUntilRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	store := frecency.NewStore(path)
	require.NoError(t, store.Record(context.Background(), "/z", "zeta"))
	state := backend.StateResult{Sessions: []backend.Session{
		{ID: "$1", Name: "alpha", Windows: []backend.Window{{ID: "@1", Name: "a", Panes: []backend.Pane{{ID: "%1", Path: "/a"}}}}},
		{ID: "$2", Name: "zeta", Windows: []backend.Window{{ID: "@2", Name: "z", Panes: []backend.Pane{{ID: "%2", Path: "/z"}}}}},
	}}
	stub := &stubBackend{state: state}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, store, nil)

	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Items, 2)
	assert.Equal(t, destinationItemID("$2", "@2", "%2"), snapshot.Items[0].ID)
	require.NoError(t, store.Record(t.Context(), "/a", "alpha"))
	require.NoError(t, store.Record(t.Context(), "/a", "alpha"))

	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})
	require.NoError(t, err)
	flat, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})

	require.NoError(t, err)
	require.Equal(t, destinationItemID("$2", "@2", "%2"), flat.Snapshot.Items[0].ID, "toggle must keep the loaded ranking")
	require.Equal(t, 1, stub.queryCalls)

	refreshed, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionRefresh})

	require.NoError(t, err)
	require.Equal(t, destinationItemID("$1", "@1", "%1"), refreshed.Snapshot.Items[0].ID)
	require.Equal(t, 2, stub.queryCalls)
}

func TestLiveAdapterRecordsOnlyAfterSuccessfulNavigation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	store := frecency.NewStore(path)
	stub := &stubBackend{state: liveState()}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, store, nil)
	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	id := snapshot.Items[0].ID
	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: id})
	require.NoError(t, err)
	stub.switchHook = func(backend.Target) {
		records, loadErr := store.Load()
		require.NoError(t, loadErr)
		assert.Empty(t, records, "navigation was recorded before the backend switch")
	}

	require.NoError(t, adapter.Navigate(context.Background(), result.Navigation))
	records, err := store.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, frecency.Record{Path: "/work/editor", Session: "core", Rank: 1, LastUsed: records[0].LastUsed}, records[0])

	failedPath := filepath.Join(t.TempDir(), "frecency.json")
	failedStore := frecency.NewStore(failedPath)
	failedBackend := &stubBackend{state: liveState(), switchErr: errors.New("switch failed")}
	failed := newLiveAdapter(func() (Backend, error) { return failedBackend, nil }, failedStore, nil)
	failedSnapshot, err := failed.Load(context.Background())
	require.NoError(t, err)
	result, err = failed.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: failedSnapshot.Items[0].ID})
	require.NoError(t, err)
	require.Error(t, failed.Navigate(context.Background(), result.Navigation))
	_, err = os.Stat(failedPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLiveAdapterRecordsTreeNavigationAtTheActivePanePath(t *testing.T) {
	store := frecency.NewStore(filepath.Join(t.TempDir(), "frecency.json"))
	stub := &stubBackend{state: liveState()}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, store, nil)
	adapter.projection = projectionTree
	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: snapshot.Items[0].Children[0].ID})
	require.NoError(t, err)
	require.NoError(t, adapter.Navigate(context.Background(), result.Navigation))

	records, err := store.Load()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "/work/editor", records[0].Path)
	assert.Equal(t, "core", records[0].Session)
}

func TestLiveAdapterNavigatesWithoutARecordablePath(t *testing.T) {
	for _, tc := range []struct {
		name       string
		projection projectionKind
		id         list.ItemID
		target     backend.Target
	}{
		{name: "session", projection: projectionTree, id: sessionItemID("$1"), target: backend.Target{SessionID: "$1"}},
		{name: "window", projection: projectionTree, id: windowItemID("$1", "@1"), target: backend.Target{SessionID: "$1", WindowID: "@1"}},
		{name: "pane", projection: projectionFlat, id: destinationItemID("$1", "@1", "%1"), target: backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "frecency.json")
			stub := &stubBackend{state: liveState()}
			adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, frecency.NewStore(path), nil)
			adapter.projection = tc.projection
			_, err := adapter.Load(t.Context())
			require.NoError(t, err)
			stub.state.Sessions[0].Windows[0].Panes[0].Path = ""

			result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: tc.id})
			require.NoError(t, err)
			err = adapter.Navigate(t.Context(), result.Navigation)

			require.NoError(t, err)
			require.Equal(t, []backend.Target{tc.target}, stub.switchCalls)
			_, err = os.Stat(path)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestLiveAdapterShowsInvalidFrecencyAndPreservesItBeforeRecording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":`), 0600))
	store := frecency.NewStore(path)
	stub := &stubBackend{state: liveState()}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, store, nil)

	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	assert.Contains(t, snapshot.Notice, "frecency state")
	require.NoError(t, os.Remove(path))
	for range 2 {
		rebound, toggleErr := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})
		require.NoError(t, toggleErr)
		require.Equal(t, snapshot.Notice, rebound.Snapshot.Notice, "toggle must retain the loaded notice without reading the repaired store")
	}
	require.NoError(t, os.WriteFile(path, []byte(`{"version":`), 0600))

	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: snapshot.Items[0].ID})
	require.NoError(t, err)
	require.NoError(t, adapter.Navigate(context.Background(), result.Navigation))

	backups, err := filepath.Glob(path + ".corrupt-*")
	require.NoError(t, err)
	require.Len(t, backups, 1)
	original, err := os.ReadFile(backups[0])
	require.NoError(t, err)
	assert.Equal(t, `{"version":`, string(original))
}

func TestLiveAdapterPropagatesFrecencyCancellation(t *testing.T) {
	adapter := newLiveAdapter(nil, frecency.NewStore(filepath.Join(t.TempDir(), "frecency.json")), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, notice, err := adapter.loadFrecency(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, notice)
}

func TestLiveAdapterSurfacesUnavailableFrecencyWithoutBlockingLoad(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, nil, errors.New("home is unavailable"))

	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	assert.Contains(t, snapshot.Notice, "home is unavailable")
	require.Len(t, snapshot.Items, 1)
}

func TestLiveAdapterBuildsOrderedContextMenusForEachItemKind(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	destinationID := destinationItemID("$1", "@1", "%1")

	for _, test := range []struct {
		name       string
		id         list.ItemID
		title      string
		projection projectionKind
		entries    []ui.MenuEntry
	}{
		{
			name: "tree session", id: sessionItemID("$1"), title: "SESSION ACTIONS", projection: projectionTree,
			entries: []ui.MenuEntry{
				{Action: ui.ActionOpen, Label: "Open session"},
				{Action: ui.ActionRenameSession, Label: "Rename session"},
				{Action: ui.ActionCreateWindow, Label: "New window"},
				{Action: ui.ActionCreateSession, Label: "New session"},
				{Action: ui.ActionDeleteSession, Label: "Delete session"},
				{Action: ui.ActionRefresh, Label: "Refresh"},
				{Action: ui.ActionToggleProjection, Label: "Toggle projection"},
			},
		},
		{
			name: "tree window", id: windowItemID("$1", "@1"), title: "WINDOW ACTIONS", projection: projectionTree,
			entries: []ui.MenuEntry{
				{Action: ui.ActionOpen, Label: "Open window"},
				{Action: ui.ActionRename, Label: "Rename window"},
				{Action: ui.ActionRenameSession, Label: "Rename session"},
				{Action: ui.ActionCreateWindow, Label: "New window"},
				{Action: ui.ActionCreateSession, Label: "New session"},
				{Action: ui.ActionDelete, Label: "Delete window"},
				{Action: ui.ActionDeleteSession, Label: "Delete session"},
				{Action: ui.ActionRefresh, Label: "Refresh"},
				{Action: ui.ActionToggleProjection, Label: "Toggle projection"},
			},
		},
		{
			name: "flat destination", id: destinationID, title: "PANE ACTIONS", projection: projectionFlat,
			entries: []ui.MenuEntry{
				{Action: ui.ActionOpen, Label: "Open pane"},
				{Action: ui.ActionRename, Label: "Rename window"},
				{Action: ui.ActionRenameSession, Label: "Rename session"},
				{Action: ui.ActionCreateWindow, Label: "New window"},
				{Action: ui.ActionCreateSession, Label: "New session"},
				{Action: ui.ActionDelete, Label: "Delete window"},
				{Action: ui.ActionDeleteSession, Label: "Delete session"},
				{Action: ui.ActionRefresh, Label: "Refresh"},
				{Action: ui.ActionToggleProjection, Label: "Toggle projection"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter.projection = test.projection
			_, err := adapter.Load(context.Background())
			require.NoError(t, err)
			result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionContextMenu, ItemID: test.id})
			require.NoError(t, err)
			require.NotNil(t, result.Menu)
			assert.Equal(t, test.title, result.Menu.Title)
			assert.Equal(t, test.entries, result.Menu.Entries)
		})
	}
}

func TestSessionActionsUseTheOwningStableSessionForEveryRowKind(t *testing.T) {
	for _, test := range []struct {
		name       string
		projection projectionKind
		id         list.ItemID
	}{
		{name: "tree session", projection: projectionTree, id: sessionItemID("$1")},
		{name: "tree window", projection: projectionTree, id: windowItemID("$1", "@1")},
		{name: "flat destination", projection: projectionFlat, id: destinationItemID("$1", "@1", "%1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &stubBackend{state: liveState()}
			adapter := testAdapter(stub)
			adapter.projection = test.projection
			_, err := adapter.Load(context.Background())
			require.NoError(t, err)

			rename := ui.ActionRequest{ActionID: ui.ActionRenameSession, ItemID: test.id}
			prompt, err := adapter.Execute(context.Background(), rename)
			require.NoError(t, err)
			require.NotNil(t, prompt.Input)
			assert.Equal(t, "RENAME SESSION", prompt.Input.Title)
			assert.Equal(t, "core", prompt.Input.InitialValue)
			stub.applyHook = func(actions []backend.Action) {
				require.Equal(t, backend.RenameSessionAction{Current: "$1", New: "platform"}, actions[0])
				stub.state.Sessions[0].Name = "platform"
			}
			rename.Value = text("platform")
			result, err := adapter.Execute(context.Background(), rename)
			require.NoError(t, err)
			assert.Equal(t, test.id, result.SelectItemID)

			remove := ui.ActionRequest{ActionID: ui.ActionDeleteSession, ItemID: test.id}
			confirmation, err := adapter.Execute(context.Background(), remove)
			require.NoError(t, err)
			require.NotNil(t, confirmation.Confirmation)
			assert.Equal(t, "DELETE SESSION", confirmation.Confirmation.Title)
			assert.Contains(t, confirmation.Confirmation.Body, "platform")
			stub.applyHook = func(actions []backend.Action) {
				require.Equal(t, backend.KillSessionAction{Name: "$1"}, actions[0])
				stub.state.Sessions = nil
			}
			remove.Confirmed = true
			result, err = adapter.Execute(context.Background(), remove)
			require.NoError(t, err)
			require.NotNil(t, result.Snapshot)
			assert.Empty(t, result.Snapshot.Items)
			assert.Empty(t, result.SelectItemID)
		})
	}
}

func TestSessionActionsRejectOriginsReplacedDuringRefresh(t *testing.T) {
	for _, row := range []struct {
		name       string
		projection projectionKind
		id         list.ItemID
	}{
		{name: "tree session", projection: projectionTree, id: sessionItemID("$1")},
		{name: "tree window", projection: projectionTree, id: windowItemID("$1", "@1")},
		{name: "flat destination", projection: projectionFlat, id: destinationItemID("$1", "@1", "%1")},
	} {
		for _, action := range []ui.ActionID{ui.ActionRenameSession, ui.ActionDeleteSession} {
			t.Run(row.name+"/"+string(action), func(t *testing.T) {
				stub := &stubBackend{state: liveState()}
				adapter := testAdapter(stub)
				adapter.projection = row.projection
				_, err := adapter.Load(context.Background())
				require.NoError(t, err)
				request := ui.ActionRequest{ActionID: action, ItemID: row.id}
				result, err := adapter.Execute(context.Background(), request)
				require.NoError(t, err)
				if action == ui.ActionRenameSession {
					require.NotNil(t, result.Input)
					request.Value = text("platform")
				} else {
					require.NotNil(t, result.Confirmation)
					request.Confirmed = true
				}

				stub.state.Sessions[0].ID = "$2"
				stub.state.Sessions[0].Windows[0].ID = "@2"
				stub.state.Sessions[0].Windows[0].Panes[0].ID = "%2"
				_, err = adapter.Load(context.Background())
				require.NoError(t, err)
				_, err = adapter.Execute(context.Background(), request)
				require.ErrorContains(t, err, "stale")
				assert.Empty(t, stub.applyCalls)
			})
		}
	}
}

func TestWindowActionsRejectSessionRowsBeforePromptOrMutation(t *testing.T) {
	for _, test := range []struct {
		name    string
		request ui.ActionRequest
	}{
		{name: "rename prompt", request: ui.ActionRequest{ActionID: ui.ActionRename, ItemID: sessionItemID("$1")}},
		{name: "rename mutation", request: ui.ActionRequest{ActionID: ui.ActionRename, ItemID: sessionItemID("$1"), Value: text("platform")}},
		{name: "delete confirmation", request: ui.ActionRequest{ActionID: ui.ActionDelete, ItemID: sessionItemID("$1")}},
		{name: "delete mutation", request: ui.ActionRequest{ActionID: ui.ActionDelete, ItemID: sessionItemID("$1"), Confirmed: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &stubBackend{state: liveState()}
			adapter := loadedAdapter(t, stub)
			result, err := adapter.Execute(context.Background(), test.request)
			require.ErrorContains(t, err, "window action")
			assert.Nil(t, result.Input)
			assert.Nil(t, result.Confirmation)
			assert.Empty(t, stub.applyCalls)
		})
	}
}

func TestLiveAdapterContextMenuOmitsActionsWithoutCapabilities(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	item := adapter.index[windowItemID("$1", "@1")]
	item.Target = backend.Target{}

	adapter.index[windowItemID("$1", "@1")] = item

	_, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionRenameSession, ItemID: windowItemID("$1", "@1")})
	require.ErrorContains(t, err, "no owning session action")
	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionContextMenu, ItemID: windowItemID("$1", "@1")})
	require.NoError(t, err)
	require.NotNil(t, result.Menu)
	assert.Equal(t, []ui.MenuEntry{
		{Action: ui.ActionCreateSession, Label: "New session"},
		{Action: ui.ActionRefresh, Label: "Refresh"},
		{Action: ui.ActionToggleProjection, Label: "Toggle projection"},
	}, result.Menu.Entries)
}

func TestLiveAdapterMenuActionFailsWhenItsOriginItemIsStale(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	request := ui.ActionRequest{ActionID: ui.ActionContextMenu, ItemID: windowItemID("$1", "@1")}
	result, err := adapter.Execute(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result.Menu)

	delete(adapter.index, windowItemID("$1", "@1"))
	_, err = adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionCreateSession, ItemID: request.ItemID})
	require.ErrorContains(t, err, "stale")
	assert.Empty(t, stub.applyCalls)
}

func TestLiveAdapterResolvesOpenFromCurrentItemIndex(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)

	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: windowItemID("$1", "@1")})
	require.NoError(t, err)
	assert.Empty(t, stub.switchCalls)
	assert.Equal(t, windowItemID("$1", "@1"), result.Navigation)
	require.NoError(t, adapter.Navigate(context.Background(), result.Navigation))
	assert.Equal(t, []backend.Target{{SessionID: "$1", WindowID: "@1"}}, stub.switchCalls)

	_, err = adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: list.ItemID{ID: "missing"}})
	require.ErrorContains(t, err, "stale")
}

func TestLiveAdapterRejectsAnItemRemovedAfterTheSnapshot(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	stub.state.Sessions[0].Windows = nil

	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: windowItemID("$1", "@1")})
	require.ErrorContains(t, err, "stale")
	assert.Empty(t, result.Navigation)
	assert.Empty(t, stub.switchCalls)
}

func TestLiveAdapterValidatesFlatPaneIdentityNotItsDirectory(t *testing.T) {
	for _, test := range []struct {
		name  string
		edit  func(*backend.Pane)
		stale bool
	}{
		{name: "pane replaced", edit: func(pane *backend.Pane) { pane.ID = "%2" }, stale: true},
		{name: "pane changed directory", edit: func(pane *backend.Pane) { pane.Path = "/work/other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &stubBackend{state: liveState()}
			adapter := loadedAdapter(t, stub)
			_, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionToggleProjection, ItemID: windowItemID("$1", "@1")})
			require.NoError(t, err)
			id := destinationItemID("$1", "@1", "%1")
			test.edit(&stub.state.Sessions[0].Windows[0].Panes[0])

			result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: id})
			if test.stale {
				require.ErrorContains(t, err, "stale")
				assert.Empty(t, result.Navigation)
			} else {
				require.NoError(t, err)
				assert.Equal(t, destinationItemID("$1", "@1", "%1"), result.Navigation)
				require.NotNil(t, adapter.pendingRecord)
				assert.Equal(t, "/work/other", adapter.pendingRecord.path)
			}
		})
	}
}

func TestLiveAdapterCreateWindowPromptsThenRefreshesAndSelectsCreatedItem(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	stub.applyHook = func(actions []backend.Action) {
		require.Equal(t, backend.CreateWindowAction{Session: "$1", Name: "logs"}, actions[0])
		stub.state.Sessions[0].Windows = append(stub.state.Sessions[0].Windows, backend.Window{ID: "@2", Index: 2, Name: "logs"})
	}
	adapter := loadedAdapter(t, stub)
	request := ui.ActionRequest{ActionID: ui.ActionCreateWindow, ItemID: windowItemID("$1", "@1")}

	prompt, err := adapter.Execute(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "CREATE WINDOW", prompt.Input.Title)

	request.Value = text("logs")
	result, err := adapter.Execute(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result.Snapshot)
	assert.Equal(t, windowItemID("$1", "@2"), result.SelectItemID)
	require.Len(t, stub.applyCalls, 1)
}

func TestLiveAdapterRenameAndDeleteReResolveTheSameStableItem(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	rename := ui.ActionRequest{ActionID: ui.ActionRename, ItemID: windowItemID("$1", "@1")}

	prompt, err := adapter.Execute(context.Background(), rename)
	require.NoError(t, err)
	assert.Equal(t, "editor", prompt.Input.InitialValue)

	stub.applyHook = func(actions []backend.Action) {
		require.Equal(t, backend.RenameWindowAction{Session: "$1", Window: "@1", WindowID: "@1", New: "api"}, actions[0])
		stub.state.Sessions[0].Windows[0].Name = "api"
	}
	rename.Value = text("api")
	result, err := adapter.Execute(context.Background(), rename)
	require.NoError(t, err)
	assert.Equal(t, windowItemID("$1", "@1"), result.SelectItemID)

	remove := ui.ActionRequest{ActionID: ui.ActionDelete, ItemID: windowItemID("$1", "@1")}
	confirmation, err := adapter.Execute(context.Background(), remove)
	require.NoError(t, err)
	assert.Equal(t, "DELETE WINDOW", confirmation.Confirmation.Title)
	stub.applyHook = func(actions []backend.Action) {
		require.Equal(t, backend.KillWindowAction{Session: "$1", Window: "@1", WindowID: "@1"}, actions[0])
		stub.state.Sessions[0].Windows = nil
	}
	remove.Confirmed = true
	result, err = adapter.Execute(context.Background(), remove)
	require.NoError(t, err)
	assert.Equal(t, sessionItemID("$1"), result.SelectItemID)
}

func TestLiveAdapterCreateSessionInheritsWorkspace(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	stub.applyHook = func(actions []backend.Action) {
		require.Equal(t, []backend.Action{
			backend.CreateSessionAction{Name: "sandbox"},
			backend.SetSessionOptionAction{Session: "sandbox", Key: backend.WorkspacePathOption, Value: "/work/.hetki.yaml"},
		}, actions)
		stub.state.Sessions = append(stub.state.Sessions, backend.Session{ID: "$2", Name: "sandbox"})
	}
	adapter := loadedAdapter(t, stub)

	prompt, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionCreateSession})
	require.NoError(t, err)
	assert.Equal(t, "CREATE SESSION", prompt.Input.Title)
	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionCreateSession, Value: text("sandbox")})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "workspace linked")
	assert.Equal(t, sessionItemID("$2"), result.SelectItemID)
}

func TestInvalidProjectionRetainsPreviousOwnerIndex(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	stub.state.Sessions = append(stub.state.Sessions, stub.state.Sessions[0])

	_, err := adapter.Load(context.Background())
	require.ErrorContains(t, err, "duplicate item ID")
	result, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: windowItemID("$1", "@1")})
	require.NoError(t, err)
	assert.Equal(t, windowItemID("$1", "@1"), result.Navigation)
}

func TestLiveAdapterTogglesOwnedStateUntilRefresh(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	destinationID := destinationItemID("$1", "@1", "%1")
	stub.state.Active = backend.ActiveContext{}
	stub.state.Sessions[0].Name = "renamed session"
	window := &stub.state.Sessions[0].Windows[0]
	window.Name = "renamed window"
	window.Panes[0].Path = "/changed/path"
	window.Panes = append(window.Panes, backend.Pane{ID: "%2", Path: "/new/pane"})

	flat, err := adapter.Execute(t.Context(), ui.ActionRequest{
		ActionID: ui.ActionToggleProjection, ItemID: windowItemID("$1", "@1"),
	})

	require.NoError(t, err)
	require.Equal(t, 1, stub.queryCalls, "toggle must not acquire inventory")
	require.Len(t, flat.Snapshot.Items, 1)
	assert.Equal(t, destinationID, flat.SelectItemID)
	assert.Equal(t, destinationID, flat.Snapshot.ActiveItemID)
	assert.Empty(t, flat.Snapshot.Items[0].Children)
	assert.Equal(t, "core", flat.Snapshot.Items[0].Context)
	assert.Equal(t, "editor", flat.Snapshot.Items[0].Primary)
	assert.Equal(t, "/work/editor", flat.Snapshot.Items[0].Secondary)

	opened, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: destinationID})
	require.NoError(t, err)
	assert.Equal(t, destinationID, opened.Navigation)
	require.Equal(t, 2, stub.queryCalls, "navigation still validates fresh inventory")
	require.NotNil(t, adapter.pendingRecord)
	assert.Equal(t, "/changed/path", adapter.pendingRecord.path)

	tree, err := adapter.Execute(t.Context(), ui.ActionRequest{
		ActionID: ui.ActionToggleProjection, ItemID: destinationID,
	})
	require.NoError(t, err)
	assert.Equal(t, windowItemID("$1", "@1"), tree.SelectItemID)
	assert.Equal(t, "core", tree.Snapshot.Items[0].Primary)
	assert.Equal(t, "1 editor", tree.Snapshot.Items[0].Children[0].Primary)
	require.Equal(t, 2, stub.queryCalls)

	_, err = adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionRefresh})
	require.NoError(t, err)
	refreshed, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})
	require.NoError(t, err)
	require.Equal(t, 3, stub.queryCalls, "only Refresh reacquires the projection input")
	require.Len(t, refreshed.Snapshot.Items, 2)
	assert.Equal(t, "renamed session", refreshed.Snapshot.Items[0].Context)
	assert.Equal(t, "renamed window", refreshed.Snapshot.Items[0].Primary)
	assert.Equal(t, "/changed/path", refreshed.Snapshot.Items[0].Secondary)
}

func TestTreeToFlatPrefersTheSelectedSessionsActiveDestination(t *testing.T) {
	state := liveState()
	state.Sessions = append(state.Sessions, backend.Session{
		ID: "$2", Name: "ops", Windows: []backend.Window{{
			ID: "@2", Name: "logs", Active: true,
			Panes: []backend.Pane{{ID: "%2", Index: 0, Path: "/ops/logs", Active: true}},
		}},
	})
	stub := &stubBackend{state: state}
	adapter := loadedAdapter(t, stub)

	result, err := adapter.Execute(context.Background(), ui.ActionRequest{
		ActionID: ui.ActionToggleProjection, ItemID: sessionItemID("$2"),
	})
	require.NoError(t, err)
	assert.Equal(t, destinationItemID("$2", "@2", "%2"), result.SelectItemID)
}

func TestFlatDestinationActionsUseTheOwningWindowAndSession(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)
	_, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionToggleProjection, ItemID: windowItemID("$1", "@1")})
	require.NoError(t, err)
	destinationID := destinationItemID("$1", "@1", "%1")

	rename := ui.ActionRequest{ActionID: ui.ActionRename, ItemID: destinationID, Value: text("api")}
	stub.applyHook = func(actions []backend.Action) {
		require.Equal(t, backend.RenameWindowAction{Session: "$1", Window: "@1", WindowID: "@1", New: "api"}, actions[0])
		stub.state.Sessions[0].Windows[0].Name = "api"
	}
	result, err := adapter.Execute(context.Background(), rename)
	require.NoError(t, err)
	assert.Equal(t, destinationID, result.SelectItemID)

	create := ui.ActionRequest{ActionID: ui.ActionCreateWindow, ItemID: destinationID, Value: text("logs")}
	stub.applyHook = func(actions []backend.Action) {
		require.Equal(t, backend.CreateWindowAction{Session: "$1", Name: "logs"}, actions[0])
		stub.state.Sessions[0].Windows = append(stub.state.Sessions[0].Windows, backend.Window{
			ID: "@2", Name: "logs", Index: 2, Panes: []backend.Pane{{ID: "%2", Path: "/work/logs"}},
		})
	}
	result, err = adapter.Execute(context.Background(), create)
	require.NoError(t, err)
	assert.Equal(t, destinationItemID("$1", "@2", "%2"), result.SelectItemID)

	remove := ui.ActionRequest{ActionID: ui.ActionDelete, ItemID: destinationID, Confirmed: true}
	stub.applyHook = func(actions []backend.Action) {
		require.Equal(t, backend.KillWindowAction{Session: "$1", Window: "@1", WindowID: "@1"}, actions[0])
		stub.state.Sessions[0].Windows = stub.state.Sessions[0].Windows[1:]
	}
	result, err = adapter.Execute(context.Background(), remove)
	require.NoError(t, err)
	assert.Empty(t, result.SelectItemID, "flat deletion must let the list choose the next or previous surviving row")

	queries := stub.queryCalls
	tree, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionToggleProjection})

	require.NoError(t, err)
	require.Equal(t, queries, stub.queryCalls)
	require.Len(t, tree.Snapshot.Items[0].Children, 1)
	assert.Equal(t, "2 logs", tree.Snapshot.Items[0].Children[0].Primary, "mutations must replace the input used by the next toggle")
}

func TestInvalidFlatProjectionRetainsTreeAndOwnerIndex(t *testing.T) {
	state := liveState()
	state.Sessions[0].Windows[0].Panes[0].ID = ""
	stub := &stubBackend{state: state}
	adapter := loadedAdapter(t, stub)

	_, err := adapter.Execute(context.Background(), ui.ActionRequest{
		ActionID: ui.ActionToggleProjection, ItemID: windowItemID("$1", "@1"),
	})
	require.ErrorContains(t, err, "pane is missing its ID")
	assert.Equal(t, projectionTree, adapter.projection)
	assert.Contains(t, adapter.index, windowItemID("$1", "@1"))
}

func TestLiveAdapterRejectsMissingStableSessionAndWindowIDsDuringProjection(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*backend.StateResult)
		want string
	}{
		{name: "session", edit: func(state *backend.StateResult) { state.Sessions[0].ID = "" }, want: "session \"core\" is missing its ID"},
		{name: "window", edit: func(state *backend.StateResult) { state.Sessions[0].Windows[0].ID = "" }, want: "window \"editor\" is missing its ID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := liveState()
			test.edit(&state)
			stub := &stubBackend{state: state}
			adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, nil, nil)

			_, err := adapter.Load(context.Background())
			require.ErrorContains(t, err, test.want)
			assert.Empty(t, adapter.index)
			assert.Empty(t, stub.applyCalls)
		})
	}
}
