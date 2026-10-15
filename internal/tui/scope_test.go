package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func scopeFixture(t *testing.T, layout string) (model, time.Time) {
	t.Helper()
	now := time.Now()
	snapshot := list.Snapshot{}
	bindings := StatusBindings{}
	values := map[status.PaneRef]status.Value{}
	for _, state := range []status.State{status.Working, status.Idle, status.Blocked, status.Unknown, status.Withdrawn, status.Unreported} {
		name := string(state)
		if state == status.Unreported {
			name = "shell"
		}
		id := list.ItemID{ID: name}
		pane := status.PaneRef{Server: "server", ID: name, Generation: "birth"}
		bindings[id] = []status.PaneRef{pane}
		if state != status.Unreported {
			values[pane] = status.Value{State: state, ValidUntil: now.Add(time.Hour)}
		}
		snapshot.Items = append(snapshot.Items, list.Item{
			ID: id, Primary: name, SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: name}},
		})
		if state == status.Unreported {
			snapshot.Items[len(snapshot.Items)-1].SearchFields = append(snapshot.Items[len(snapshot.Items)-1].SearchFields,
				list.SearchField{Tier: list.SearchPrimary, Text: "working"})
		}
	}
	if layout == "tree" {
		snapshot.Items[0].ID = list.ItemID{ID: "mixed"}
		bindings[list.ItemID{ID: "mixed"}] = append(bindings[list.ItemID{ID: "working"}], bindings[list.ItemID{ID: "blocked"}]...)
		delete(bindings, list.ItemID{ID: "working"})
		delete(bindings, list.ItemID{ID: "blocked"})
		snapshot.Items = append(snapshot.Items[:2], snapshot.Items[3:]...)
		snapshot.Items = []list.Item{{ID: list.ItemID{ID: "session"}, Primary: "dev", Children: snapshot.Items}}
	}
	m := newModel(snapshot, nil)
	m.paneStatus.options.Bindings = bindings
	var err error
	m.paneStatus.values, err = status.NewSnapshot(values)
	require.NoError(t, err)
	return m, now
}

func scopeRowIDs(m model) []list.ItemID {
	ids := make([]list.ItemID, 0, len(m.items.Rows()))
	for _, row := range m.items.Rows() {
		ids = append(ids, row.Item.ID)
	}
	return ids
}

func TestScopeAndTextSearch(t *testing.T) {
	for _, test := range []struct {
		name    string
		layout  string
		tracked bool
		query   string
		want    []list.ItemID
	}{
		{"all panes", "flat", false, "", []list.ItemID{{ID: "working"}, {ID: "idle"}, {ID: "blocked"}, {ID: "unknown"}, {ID: "withdrawn"}, {ID: "shell"}}},
		{"tracked includes unknown", "flat", true, "", []list.ItemID{{ID: "working"}, {ID: "idle"}, {ID: "blocked"}, {ID: "unknown"}}},
		{"text search within tracked", "flat", true, "idle", []list.ItemID{{ID: "idle"}}},
		{"plain status name is text", "flat", false, "working", []list.ItemID{{ID: "working"}, {ID: "shell"}}},
		{"tree retains ancestors", "tree", true, "", []list.ItemID{{ID: "session"}, {ID: "mixed"}, {ID: "idle"}, {ID: "unknown"}}},
		{"tree text search retains ancestors", "tree", true, "idle", []list.ItemID{{ID: "session"}, {ID: "idle"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, now := scopeFixture(t, test.layout)
			m.scope.tracked = test.tracked

			m.applyScope(test.query, now)

			require.Equal(t, test.query, m.items.Query())
			require.Equal(t, test.want, scopeRowIDs(m))
		})
	}
}

func TestTrackedScopeKeepsBindingsAndSelectionByStructuralID(t *testing.T) {
	first := list.ItemID{Scope: "first scope", Parent: "parent", ID: "shared"}
	second := list.ItemID{Scope: "second scope", Parent: "parent", ID: "shared"}
	third := list.ItemID{Scope: "second scope", Parent: "other parent", ID: "shared"}
	m, err := newModelWithStartMode(list.Snapshot{Items: []list.Item{
		{ID: first, Primary: "first row"},
		{ID: second, Primary: "second row"},
		{ID: third, Primary: "third row"},
	}}, nil, DefaultKeyMap(), StartModeNormal)
	require.NoError(t, err)
	pane := status.PaneRef{Server: "server", ID: "tracked", Generation: "birth"}
	m.paneStatus.options.Bindings = StatusBindings{second: {pane}}
	m.paneStatus.values, err = status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Idle, ValidUntil: time.Now().Add(time.Hour)},
	})
	require.NoError(t, err)

	tracked, _ := updateModel(t, m, specialKey(tea.KeyTab))
	restored, _ := updateModel(t, tracked, specialKey(tea.KeyTab))

	require.Equal(t, []list.ItemID{second}, scopeRowIDs(tracked))
	require.Equal(t, second, selectedNodeID(tracked))
	require.Equal(t, []list.ItemID{first, second, third}, scopeRowIDs(restored))
	require.Equal(t, first, selectedNodeID(restored))
}

func TestStatusPrefixIsOrdinarySearchText(t *testing.T) {
	m := newModel(list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "literal"}, Primary: "status:working",
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "status:working"}},
	}}}, nil)

	m.setQuery("status:working")

	require.Equal(t, "status:working", m.items.Query())
	require.Equal(t, []list.ItemID{{ID: "literal"}}, scopeRowIDs(m))
}

func TestScopeToggleIsLocalAndPreservesQueryAndSelection(t *testing.T) {
	for _, mode := range []uiMode{modeBrowse, modeFilter, modeJump} {
		t.Run(string(mode), func(t *testing.T) {
			m, now := scopeFixture(t, "flat")
			m.applyScope("idle", now)
			m.mode = mode
			reads := 0
			m.paneStatus.options.Read = func(_ context.Context, previous status.Snapshot) StatusUpdate {
				reads++
				return StatusUpdate{Values: previous}
			}

			next, cmd := m.updateKey(specialKey(tea.KeyTab))
			tracked := next.(model)
			returned, returnCmd := tracked.updateKey(specialKey(tea.KeyTab))
			all := returned.(model)

			require.Nil(t, cmd)
			require.Nil(t, returnCmd)
			require.Zero(t, reads)
			require.True(t, tracked.scope.tracked)
			require.False(t, all.scope.tracked)
			require.Equal(t, "idle", all.items.Query())
			require.Equal(t, list.ItemID{ID: "idle"}, selectedNodeID(all))
			require.Equal(t, mode, all.mode)
		})
	}
}

func TestLayoutSwitchPreservesScopeAndQuery(t *testing.T) {
	m, now := scopeFixture(t, "flat")
	tree, _ := scopeFixture(t, "tree")
	replacement := tree.items.Snapshot()
	var requested ActionID
	m.dispatch = func(request ActionRequest) (ActionResult, error) {
		requested = request.ActionID
		return ActionResult{
			Snapshot: &replacement, Status: &StatusUpdate{Values: tree.paneStatus.values, Bindings: tree.paneStatus.options.Bindings}, SelectItemID: list.ItemID{ID: "idle"},
		}, nil
	}
	m.scope.tracked = true
	m.paneStatus.values = status.Snapshot{}
	m.applyScope("idle", now)
	m.mode = modeFilter

	pending, cmd := m.updateKey(controlKey('t'))
	completed, _ := pending.(model).handleActionResult(cmd().(actionResultMsg))
	m = completed.(model)

	require.Equal(t, ActionToggleProjection, requested)
	require.NoError(t, m.err)
	require.True(t, m.scope.tracked)
	require.Equal(t, modeFilter, m.mode)
	require.Equal(t, "idle", m.items.Query())
	require.Equal(t, []list.ItemID{{ID: "session"}, {ID: "idle"}}, scopeRowIDs(m))
	require.Equal(t, list.ItemID{ID: "idle"}, selectedNodeID(m))
}

func TestStatusMembershipChangeCancelsJumpInsteadOfRelabeling(t *testing.T) {
	m, now := scopeFixture(t, "flat")
	m.scope.tracked = true
	m.applyScope("", now)
	jumping, _ := handleJump(m, list.ItemID{})
	m = jumping.(model)
	m.jump.input = "a"

	next, _ := m.handleStatusResult(statusResultMsg{update: StatusUpdate{Values: status.Snapshot{}}})
	m = next.(model)

	require.Equal(t, modeBrowse, m.mode)
	require.Empty(t, m.jump.candidates)
	require.Empty(t, m.items.Rows())
}

func TestScopeWithoutProviderHasNoTrackedItems(t *testing.T) {
	m := newModel(viewSnapshot(), nil)

	next, cmd := handleToggleScope(m, list.ItemID{})
	m = next.(model)

	require.Nil(t, cmd)
	require.Empty(t, m.items.Rows())
	require.Equal(t, "no matching tracked items", emptyStateText(m))
}

func TestScopesRememberTheirSelectionAndExpansion(t *testing.T) {
	m, _ := scopeFixture(t, "tree")
	m.mode = modeBrowse
	m.items.CollapseAll()

	tracked, _ := handleToggleScope(m, list.ItemID{})
	m = tracked.(model)
	trackedRows := scopeRowIDs(m)
	m.items.Select(list.ItemID{ID: "idle"})
	all, _ := handleToggleScope(m, list.ItemID{})
	returned, _ := handleToggleScope(all.(model), list.ItemID{})

	require.Equal(t, []list.ItemID{{ID: "session"}, {ID: "mixed"}, {ID: "idle"}, {ID: "unknown"}}, trackedRows)
	require.Equal(t, []list.ItemID{{ID: "session"}}, scopeRowIDs(all.(model)))
	require.Equal(t, list.ItemID{ID: "session"}, selectedNodeID(all.(model)))
	require.Equal(t, list.ItemID{ID: "idle"}, selectedNodeID(returned.(model)))
}

func TestScopeKeysRespectModes(t *testing.T) {
	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"scope", specialKey(tea.KeyTab)},
		{"layout", controlKey('t')},
	} {
		for _, mode := range []uiMode{modeInput, modeConfirm, modeMenu} {
			t.Run(test.name+"/"+string(mode), func(t *testing.T) {
				m, _ := scopeFixture(t, "flat")
				m.mode = mode
				before := selectedNodeID(m)

				next, cmd := m.updateKey(test.key)
				m = next.(model)

				require.Nil(t, cmd)
				require.False(t, m.scope.tracked)
				require.Equal(t, mode, m.mode)
				require.Equal(t, before, selectedNodeID(m))
			})
		}
	}
}
