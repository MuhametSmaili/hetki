package tui

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func replacementWorker(t *testing.T, m model) model {
	t.Helper()
	m.search.worker = newSearchWorker(t.Context(), nil)
	m.items.DeferSearch()
	t.Cleanup(m.search.worker.close)
	return m
}

func finishReplacement(t *testing.T, m model) model {
	t.Helper()
	require.NotNil(t, m.replacement)
	msg := receiveSearch(t, m.search.worker.results)
	require.NotNil(t, msg.replacement)
	require.NoError(t, msg.err)
	m, _ = updateModel(t, m, msg)
	require.Nil(t, m.replacement)
	return m
}

func TestPreparedReplacementPreservesActionSelection(t *testing.T) {
	for _, action := range []ActionID{ActionRefresh, ActionCreateSession, ActionDelete} {
		for _, query := range []string{"", "server"} {
			t.Run(string(action)+"/"+query, func(t *testing.T) {
				m := newModel(interactionSnapshot(), nil)
				m.setQuery(query)
				m.pending = &ActionRequest{ActionID: action, ItemID: list.ItemID{ID: "window-2"}}
				m.pendingRows = m.items.RowIDs()
				snapshot := interactionSnapshot()
				snapshot.Items[0].Children = snapshot.Items[0].Children[:1]
				result := ActionResult{Snapshot: &snapshot}
				if action == ActionCreateSession {
					id := list.ItemID{ID: "created"}
					snapshot.Items = append(snapshot.Items, list.Item{ID: id, Primary: "new session", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "new session"}}})
					result.SelectItemID = id
				}
				msg := actionResultMsg{result: result}
				want, _ := updateModel(t, m, msg)
				require.NoError(t, want.err)
				m = replacementWorker(t, m)
				previous := m.items.Rows()

				m, _ = updateModel(t, m, msg)
				require.True(t, m.busy)
				require.Equal(t, previous, m.items.Rows())
				m = finishReplacement(t, m)

				require.NoError(t, m.err)
				require.False(t, m.busy)
				require.False(t, m.items.SearchPending())
				require.Equal(t, want.items.Rows(), m.items.Rows())
				require.Equal(t, want.items.Query(), m.items.Query())
				require.Equal(t, selectedNodeID(want), selectedNodeID(m))
			})
		}
	}
}

func TestReplacementRejectsLateResultsAndKeepsLiveViewport(t *testing.T) {
	m := replacementWorker(t, newModel(interactionSnapshot(), nil))
	first, second := flatSnapshot(3), flatSnapshot(5)
	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &first}})
	late := receiveSearch(t, m.search.worker.results)
	m.paneStatus.reading = true
	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &second}})

	m, _ = updateModel(t, m, late)
	require.True(t, m.busy)
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 74, Height: 18})
	m, _ = updateModel(t, m, statusResultMsg{})
	m = finishReplacement(t, m)

	require.Equal(t, 5, m.items.Len())
	require.Equal(t, 74, m.width)
	require.Equal(t, 18, m.height)
	require.False(t, m.paneStatus.reading)

	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &first}})
	late = receiveSearch(t, m.search.worker.results)
	failure := errors.New("superseding action failed")
	m, _ = updateModel(t, m, actionResultMsg{err: failure})
	m, _ = updateModel(t, m, late)
	require.ErrorIs(t, m.err, failure)
	require.False(t, m.busy)
	require.Equal(t, 5, m.items.Len())
}

func TestChoicePreparationRetainsUsableParentAfterWorkerCancellation(t *testing.T) {
	m := replacementWorker(t, newModel(interactionSnapshot(), nil))
	parent := m.items.Rows()
	m, _ = updateModel(t, m, actionResultMsg{result: choiceResult("actions", "editor")})
	m = finishReplacement(t, m)
	require.NotNil(t, m.choice.parent)

	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Choice: &ChoiceScreen{Closed: true}}})
	m.setQuery("server")
	m.setQuery("")

	require.Nil(t, m.choice)
	require.Equal(t, parent, m.items.Rows())
	require.False(t, m.items.SearchPending())
}

func TestReplacementCancelsRunningSearchAndQuitRejectsPreparedResult(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	m := newModel(interactionSnapshot(), nil)
	m.items.DeferSearch()
	m.search.worker = newSearchWorker(t.Context(), func(ctx context.Context, _ list.SearchRequest) (list.SearchResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return list.SearchResult{}, ctx.Err()
	})
	t.Cleanup(m.search.worker.close)
	m.setQuery("server")
	m.syncSearch()
	receiveSearch(t, started)
	snapshot := flatSnapshot(4)

	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &snapshot}})
	receiveSearch(t, cancelled)
	prepared := receiveSearch(t, m.search.worker.results)
	m, _ = updateModel(t, m, controlKey('c'))
	previous := m.items.Rows()
	m, _ = updateModel(t, m, prepared)

	require.True(t, m.quitting)
	require.Nil(t, m.replacement)
	require.Equal(t, previous, m.items.Rows())
}

func TestReplacementFailurePreservesOldRows(t *testing.T) {
	m := replacementWorker(t, newModel(interactionSnapshot(), nil))
	before := m.items.Rows()
	invalid := list.Snapshot{Items: []list.Item{{Primary: "missing identity"}}}
	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &invalid}})
	m = finishReplacement(t, m)
	require.Error(t, m.err)
	require.Equal(t, before, m.items.Rows())

	failure := errors.New("source failed")
	m, _ = updateModel(t, m, actionResultMsg{result: choiceResult("actions", "retry"), err: failure})
	m = finishReplacement(t, m)
	require.ErrorIs(t, m.err, failure)
	require.NotNil(t, m.choice)
	require.Equal(t, 1, m.items.Len())
}

func TestPreparedTrackedReplacementPreservesScopeAndExpansion(t *testing.T) {
	m, now := scopeFixture(t, "tree")
	m.scope.mode = ScopeTracked
	m.applyScope("idle", now)
	snapshot := m.items.Snapshot()
	msg := actionResultMsg{result: ActionResult{
		Snapshot: &snapshot, View: ViewTree,
		Status: &StatusUpdate{Values: m.paneStatus.values, Bindings: m.paneStatus.options.Bindings},
	}}
	want, _ := updateModel(t, m, msg)
	m = replacementWorker(t, m)

	m, _ = updateModel(t, m, msg)
	m = finishReplacement(t, m)

	require.NoError(t, m.err)
	require.Equal(t, want.items.Rows(), m.items.Rows())
	require.Equal(t, want.items.Expansion(), m.items.Expansion())
	require.Equal(t, want.scope.mode, m.scope.mode)
	require.Equal(t, selectedNodeID(want), selectedNodeID(m))
}

func TestReplacementRepreparesStatusThatExpiresBeforeDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, now := scopeFixture(t, "flat")
		pane := m.paneStatus.options.Bindings[list.ItemID{ID: "idle"}][0]
		value := status.Value{State: status.Unknown, Outcome: status.Completed, OutcomeEnded: true,
			OutcomeSince: now, ValidUntil: now.Add(time.Hour)}
		values, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: value})
		require.NoError(t, err)
		m.scope.mode = ScopeTracked
		m.applyStatusUpdate(StatusUpdate{Values: values})
		m.setQuery("")
		m = replacementWorker(t, m)
		snapshot := m.items.Snapshot()
		msg := actionResultMsg{result: ActionResult{Snapshot: &snapshot,
			Status: &StatusUpdate{Values: values, Bindings: m.paneStatus.options.Bindings}}}
		m, _ = updateModel(t, m, msg)
		prepared := receiveSearch(t, m.search.worker.results)

		time.Sleep(value.NextChange(now).Sub(now))
		m, _ = updateModel(t, m, prepared)
		require.True(t, m.busy)
		m = finishReplacement(t, m)

		require.Empty(t, m.items.Rows())
	})
}
