package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func completeSearch(t *testing.T, m model) model {
	t.Helper()
	request, pending := m.items.PendingSearch()
	require.True(t, pending)
	result, err := request.Evaluate(t.Context())
	require.NoError(t, err)
	m, _ = updateModel(t, m, searchResultMsg{key: request.Key(), result: result})
	return m
}

func TestPendingSearchBlocksRowActionsAndAcceptsOnlyLatestQuery(t *testing.T) {
	m := newModel(interactionSnapshot(), func(ActionRequest) (ActionResult, error) { return ActionResult{}, nil })
	m.items.DeferSearch()
	previous := m.items.RowIDs()
	m, _ = updateModel(t, m, printableKey("editor"))
	oldRequest, _ := m.items.PendingSearch()
	oldResult, err := oldRequest.Evaluate(t.Context())
	require.NoError(t, err)

	for _, action := range []ActionID{ActionOpen, ActionConfirm, ActionDelete, ActionContextMenu, ActionUserActions} {
		next, cmd := m.dispatchAction(action, list.ItemID{})
		require.Nil(t, cmd)
		require.False(t, next.(model).busy)
	}
	m, _ = updateModel(t, m, specialKey(tea.KeyEnter))
	require.Equal(t, modeFilter, m.mode, "ignored Enter must leave typing active")
	m.setQuery("server")
	m, _ = updateModel(t, m, searchResultMsg{key: oldRequest.Key(), result: oldResult})

	require.True(t, m.items.SearchPending())
	require.Equal(t, previous, m.items.RowIDs())
	require.Contains(t, headerMessage(m), "previous results")
	require.True(t, selectedNodeID(m).IsZero(), "old rows must not become action/preview targets")
	m = completeSearch(t, m)
	require.False(t, m.items.SearchPending())
	require.Equal(t, list.ItemID{ID: "window-2"}, selectedNodeID(m))
	m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))
	require.NotNil(t, cmd)
	require.Equal(t, list.ItemID{ID: "window-2"}, m.pending.ItemID)
}

func TestClearSearchRestoresRowsWithoutWaitingForWorker(t *testing.T) {
	m := newModel(interactionSnapshot(), nil)
	m.items.DeferSearch()
	want := m.items.RowIDs()
	m.setQuery("missing")
	request, _ := m.items.PendingSearch()
	late, err := request.Evaluate(t.Context())
	require.NoError(t, err)

	m.setQuery("")
	m, _ = updateModel(t, m, searchResultMsg{key: request.Key(), result: late})

	require.False(t, m.items.SearchPending())
	require.Equal(t, want, m.items.RowIDs())
	require.Empty(t, m.items.Query())
	require.False(t, selectedNodeID(m).IsZero())
}

func TestAsyncSnapshotSelectionMatchesSynchronousBehavior(t *testing.T) {
	for _, action := range []ActionID{ActionDelete, ActionCreateSession, ActionRefresh} {
		t.Run(string(action), func(t *testing.T) {
			m := newModel(interactionSnapshot(), nil)
			m.setQuery("server")
			m.pending = &ActionRequest{ActionID: action, ItemID: list.ItemID{ID: "window-2"}}
			m.pendingRows = m.items.RowIDs()
			snapshot := interactionSnapshot()
			snapshot.Items[0].Children = snapshot.Items[0].Children[:1]
			created := list.ItemID{ID: "new-session"}
			result := ActionResult{Snapshot: &snapshot}
			if action == ActionCreateSession {
				snapshot.Items = append(snapshot.Items, list.Item{ID: created, Primary: "new session", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "new session"}}})
				result.SelectItemID = created
			}
			async := m
			async.items.DeferSearch()

			want, _ := updateModel(t, m, actionResultMsg{result: result})
			async, _ = updateModel(t, async, actionResultMsg{result: result})
			async = completeSearch(t, async)

			require.False(t, async.items.SearchPending())
			require.Equal(t, want.items.Query(), async.items.Query())
			require.Equal(t, want.items.RowIDs(), async.items.RowIDs())
			require.Equal(t, selectedNodeID(want), selectedNodeID(async))
		})
	}
}

func TestChoiceScreenKeepsDeferredSearchAcrossTransitions(t *testing.T) {
	m := newModel(interactionSnapshot(), nil)
	m.search.worker = newSearchWorker(t.Context(), func(ctx context.Context, request list.SearchRequest) (list.SearchResult, error) {
		return request.Evaluate(ctx)
	})
	t.Cleanup(m.search.worker.close)
	m.items.DeferSearch()
	m.setQuery("editor")
	oldRequest, _ := m.items.PendingSearch()
	oldResult, err := oldRequest.Evaluate(t.Context())
	require.NoError(t, err)

	updated, _ := m.applyChoiceResult(choiceResult("choices", "editor"), nil)
	m = updated.(model)
	m.setQuery("editor")
	m, _ = updateModel(t, m, searchResultMsg{key: oldRequest.Key(), result: oldResult})
	require.True(t, m.items.SearchPending())
	m = completeSearch(t, m)

	require.False(t, m.items.SearchPending())
	require.Len(t, m.items.RowIDs(), 1)
	sameKey := oldRequest.Key() == m.search.requested
	require.False(t, sameKey, "new choice screens must have independent search identities")
}

func TestEnterWaitsForItsExactSearchResult(t *testing.T) {
	m := newModel(interactionSnapshot(), func(ActionRequest) (ActionResult, error) { return ActionResult{}, nil })
	m.items.DeferSearch()
	m, _ = updateModel(t, m, printableKey("server"))

	m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))
	require.Nil(t, cmd)
	require.False(t, m.busy)
	m = completeSearch(t, m)

	require.True(t, m.busy)
	require.NotNil(t, m.pending)
	require.Equal(t, list.ItemID{ID: "window-2"}, m.pending.ItemID)
}

func TestCreatedSessionKeepsQueryWhenLeavingTrackedScope(t *testing.T) {
	created := list.ItemID{ID: "new-session"}
	snapshot := interactionSnapshot()
	snapshot.Items = append(snapshot.Items, list.Item{ID: created, Primary: "new session",
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "new session"}}})
	m := newModel(snapshot, nil)
	m.scope.mode = ScopeTracked
	m.setQuery("new")
	m.items.DeferSearch()

	m.revealCreatedSession(created, &ActionRequest{ActionID: ActionCreateSession})
	m = completeSearch(t, m)

	require.Equal(t, ScopeAll, m.scope.mode)
	require.Equal(t, "new", m.items.Query())
	require.Equal(t, created, selectedNodeID(m))
}

func TestPendingEnterDoesNotCarryAcrossDatasetReplacement(t *testing.T) {
	m := newModel(interactionSnapshot(), nil)
	m.items.DeferSearch()
	m.setQuery("server")
	m, _ = updateModel(t, m, specialKey(tea.KeyEnter))
	request, _ := m.items.PendingSearch()
	late, err := request.Evaluate(t.Context())
	require.NoError(t, err)

	require.NoError(t, m.items.Replace(interactionSnapshot(), list.ItemID{}))
	m, _ = updateModel(t, m, searchResultMsg{key: request.Key(), result: late})
	m = completeSearch(t, m)

	require.False(t, m.busy)
	require.Nil(t, m.pending)
	require.Equal(t, "server", m.items.Query())
}
