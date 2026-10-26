package tui

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func windowStatusFixture(t *testing.T) (model, []WindowPane) {
	t.Helper()
	panes := []WindowPane{
		{PaneID: "%1", Ref: status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}, Active: true,
			Details: list.Details{Secondary: "/shell"}},
		{PaneID: "%2", Ref: status.PaneRef{Server: "server", ID: "%2", Generation: "birth"},
			Details: list.Details{Secondary: "/agent", Trailing: "opencode", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "opencode"}}}},
		{PaneID: "%3", Ref: status.PaneRef{Server: "server", ID: "%3", Generation: "birth"},
			Details: list.Details{Secondary: "/editor", Trailing: "pi", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "pi"}}}},
	}
	m := newModel(list.Snapshot{Items: []list.Item{{ID: list.ItemID{ID: "session"}, Primary: "session", Children: []list.Item{
		{ID: list.ItemID{ID: "window"}, Primary: "1 agents", Secondary: "/shell"},
	}}}}, nil)
	m.paneStatus.windows = map[list.ItemID][]WindowPane{{ID: "window"}: panes}
	m.paneStatus.options.Bindings = StatusBindings{{ID: "window"}: {panes[0].Ref, panes[1].Ref, panes[2].Ref}}
	return m, panes
}

func TestTreeRowFollowsOnePaneAcrossStatusChanges(t *testing.T) {
	now := time.Now()
	id := list.ItemID{ID: "window"}
	for _, tc := range []struct {
		name     string
		states   [3]status.State
		previous int
		want     int
	}{
		{"inactive agent works", [3]status.State{status.Idle, status.Working, status.Idle}, 0, 1},
		{"another agent needs input", [3]status.State{status.Idle, status.Working, status.Blocked}, 1, 2},
		{"equal priorities retain the later pane", [3]status.State{status.Idle, status.Working, status.Working}, 2, 2},
		{"idle agent is shown over a shell without status", [3]status.State{status.Unreported, status.Idle, status.Idle}, 0, 1},
		{"equal idle retains the previous pane", [3]status.State{status.Unreported, status.Idle, status.Idle}, 2, 2},
		{"no status shows the active pane", [3]status.State{status.Unreported, status.Unreported, status.Unreported}, 2, 0},
		{"unknown retains its own command", [3]status.State{status.Idle, status.Unknown, status.Idle}, 0, 1},
		{"withdrawal returns to active shell", [3]status.State{status.Idle, status.Withdrawn, status.Idle}, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, panes := windowStatusFixture(t)
			m.paneStatus.representatives = map[list.ItemID]windowRepresentative{id: {ref: panes[tc.previous].Ref}}
			require.True(t, m.items.Select(id))
			values := make(map[status.PaneRef]status.Value)
			for i, state := range tc.states {
				if state != status.Unreported {
					values[panes[i].Ref] = status.Value{State: state, ValidUntil: now.Add(time.Minute)}
				}
			}
			snapshot, err := status.NewSnapshot(values)
			require.NoError(t, err)

			next, _ := m.handleStatusResult(statusResultMsg{update: StatusUpdate{Values: snapshot}})
			m = next.(model)

			row, ok := m.items.Selected()
			require.True(t, ok)
			require.Equal(t, id, row.Item.ID)
			require.Equal(t, "1 agents", row.Item.Primary)
			require.Equal(t, panes[tc.want].Details.Trailing, row.Item.Trailing)
			require.Equal(t, panes[tc.want].Details.Secondary, row.Item.Secondary)
			require.Equal(t, tc.states[tc.want], m.paneStatus.rowValue(id, now).State)
		})
	}
}

func TestTrackedWindowsShowTheStatusTheyAreTrackedFor(t *testing.T) {
	now := time.Now()
	id := list.ItemID{ID: "window"}
	for _, tc := range []struct {
		name    string
		state   status.State
		tracked bool
	}{
		{"idle inactive pane", status.Idle, true},
		{"no status", status.Unreported, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, panes := windowStatusFixture(t)
			values := map[status.PaneRef]status.Value{}
			if tc.state != status.Unreported {
				values[panes[1].Ref] = status.Value{State: tc.state, ValidUntil: now.Add(time.Minute)}
			}
			snapshot, err := status.NewSnapshot(values)
			require.NoError(t, err)
			m.paneStatus.values = snapshot

			m.applyStatusPresentation(now)
			_, tracked := m.trackedItems(now)[id]
			row := m.paneStatus.rowValue(id, now)

			require.Equal(t, tc.tracked, tracked)
			require.Equal(t, tc.state, row.State)
		})
	}
}

func TestTreeStatusExpiryReprojectsFilteredRows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		m, panes := windowStatusFixture(t)
		values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
			panes[0].Ref: {State: status.Working, ValidUntil: now.Add(time.Hour)},
			panes[1].Ref: {State: status.Blocked, ValidUntil: now.Add(statusSuccessDuration)},
		})
		require.NoError(t, err)
		m.applyStatusUpdate(StatusUpdate{Values: values})
		m.applyStatusPresentation(now)
		m.applyScope("opencode", now)
		require.Len(t, m.items.Rows(), 2)
		m.applyScope("no-match", now)
		require.Empty(t, m.items.Rows())
		due := m.paneStatus.nextWindowStatusChange(now)
		require.Equal(t, now.Add(statusSuccessDuration), due)

		time.Sleep(statusSuccessDuration)
		next, cmd := m.update(statusFrameMsg{generation: m.paneStatus.frameGeneration})
		m = next.(model)
		m.setQuery("")

		require.Nil(t, cmd, "expiry is local, with no backend/status read")
		require.Empty(t, m.items.Rows()[1].Item.Trailing)
		require.Equal(t, "/shell", m.items.Rows()[1].Item.Secondary)
		require.Equal(t, status.Working, m.paneStatus.rowValue(list.ItemID{ID: "window"}, time.Now()).State)
	})
}

func TestRepresentativeRejectsMissingOrReplacedPane(t *testing.T) {
	now := time.Now()
	_, panes := windowStatusFixture(t)
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		panes[1].Ref: {State: status.Blocked, ValidUntil: now.Add(time.Minute)},
		panes[2].Ref: {State: status.Working, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	previous := panes[1].Ref
	for _, tc := range []struct {
		name  string
		panes []WindowPane
		want  int
	}{
		{"removed", []WindowPane{panes[0], panes[2]}, 1},
		{"new incarnation", []WindowPane{panes[0], {Ref: status.PaneRef{Server: "server", ID: "%2", Generation: "new"}}, panes[2]}, 2},
		{"no active or evidence", []WindowPane{{}}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := representativePane(tc.panes, previous, values, now)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestTreeRepresentativeSurvivesRefreshAndViewToggle(t *testing.T) {
	now := time.Now()
	m, panes := windowStatusFixture(t)
	id := list.ItemID{ID: "window"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		panes[1].Ref: {State: status.Working, ValidUntil: now.Add(time.Minute)},
		panes[2].Ref: {State: status.Working, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	m.applyStatusUpdate(StatusUpdate{Values: values})
	m.applyStatusPresentation(now)
	require.Equal(t, panes[1].Ref, m.paneStatus.representatives[id].ref)
	tree := m.items.Snapshot()
	reversed := slices.Clone(panes)
	slices.Reverse(reversed)
	reversed[1].Details.Trailing = "renamed-agent"
	updated := StatusUpdate{Values: values, Windows: map[list.ItemID][]WindowPane{id: reversed}}

	next, _ := m.handleActionResult(actionResultMsg{result: ActionResult{Snapshot: &tree, Status: &updated}})
	m = next.(model)

	require.Equal(t, panes[1].Ref, m.paneStatus.representatives[id].ref, "reordered equal-priority panes keep their representative")
	require.Equal(t, "renamed-agent", m.items.Rows()[1].Item.Trailing)
	m.items.CollapseAll()
	m.applyScope("", now)
	require.Len(t, m.items.Rows(), 1, "status presentation preserves collapsed windows")
	flat := list.Snapshot{Items: []list.Item{{ID: list.ItemID{ID: "pane"}, Primary: "flat"}}}
	next, _ = m.handleActionResult(actionResultMsg{result: ActionResult{Snapshot: &flat}})
	m = next.(model)
	next, _ = m.handleActionResult(actionResultMsg{result: ActionResult{Snapshot: &tree, Status: &updated}})
	m = next.(model)

	require.Equal(t, panes[1].Ref, m.paneStatus.representatives[id].ref)
	require.Equal(t, status.Working, m.paneStatus.rowValue(id, now).State)
}

func TestFilteredTreeRefreshPreservesSelectionAfterDetailsReturn(t *testing.T) {
	first := list.ItemID{ID: "window-one"}
	second := list.ItemID{ID: "window-two"}
	cases := []struct {
		name           string
		preferred      list.ItemID
		removeSelected bool
		hideSelected   bool
		want           list.ItemID
	}{
		{"keep current selection", list.ItemID{}, false, false, second},
		{"explicit selection takes priority", first, false, false, first},
		{"removed selection falls back", list.ItemID{}, true, false, first},
		{"filtered-out selection falls back", list.ItemID{}, false, true, first},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := list.Snapshot{Items: []list.Item{{
				ID: list.ItemID{ID: "session"}, Primary: "session",
				Children: []list.Item{
					{ID: first, Primary: "one", Secondary: "/shell"},
					{ID: second, Primary: "two", Secondary: "/shell"},
				},
			}}}
			windows := make(map[list.ItemID][]WindowPane)
			values := make(map[status.PaneRef]status.Value)
			for _, id := range []list.ItemID{first, second} {
				ref := status.PaneRef{Server: "server", ID: id.ID, Generation: "birth"}
				values[ref] = status.Value{State: status.Blocked, ValidUntil: time.Now().Add(time.Minute)}
				windows[id] = []WindowPane{
					{Active: true, Details: list.Details{Secondary: "/shell"}},
					{Ref: ref, Details: list.Details{Trailing: "opencode", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "opencode"}}}},
				}
			}
			snapshot, err := status.NewSnapshot(values)
			require.NoError(t, err)
			update := StatusUpdate{Values: snapshot, Windows: windows}
			m := newModel(tree, func(request ActionRequest) (ActionResult, error) {
				require.Equal(t, ActionRequest{ActionID: ActionRefresh}, request)
				replacement := tree
				if tc.removeSelected {
					replacement.Items = slices.Clone(tree.Items)
					replacement.Items[0].Children = tree.Items[0].Children[:1]
					update.Windows = map[list.ItemID][]WindowPane{first: windows[first]}
				}
				if tc.hideSelected {
					update.Windows = map[list.ItemID][]WindowPane{
						first:  windows[first],
						second: {{Active: true, Details: list.Details{Secondary: "/shell"}}},
					}
				}
				return ActionResult{Snapshot: &replacement, Status: &update, SelectItemID: tc.preferred}, nil
			})
			m, _ = updateModel(t, m, statusResultMsg{update: update})
			m, _ = updateModel(t, m, printableKey("opencode"))
			m, _ = updateModel(t, m, specialKey(tea.KeyEscape))
			m, _ = updateModel(t, m, specialKey(tea.KeyDown))
			require.Equal(t, second, selectedNodeID(m))

			m, cmd := updateModel(t, m, controlKey('r'))
			require.NotNil(t, cmd)
			m, _ = updateModel(t, m, cmd())

			require.NoError(t, m.err)
			require.False(t, m.busy)
			require.Equal(t, "opencode", m.items.Query())
			require.Equal(t, tc.want, selectedNodeID(m))
		})
	}
}

func TestRepresentativePrefersActivePaneOnInitialTie(t *testing.T) {
	now := time.Now()
	_, panes := windowStatusFixture(t)
	panes[0].Active = false
	panes[2].Active = true
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		panes[1].Ref: {State: status.Working, ValidUntil: now.Add(time.Minute)},
		panes[2].Ref: {State: status.Working, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)

	got := representativePane(panes, status.PaneRef{}, values, now)

	require.Equal(t, 2, got)
}
