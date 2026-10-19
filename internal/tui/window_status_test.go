package tui

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func windowStatusFixture(t *testing.T) (model, []WindowPane) {
	t.Helper()
	panes := []WindowPane{
		{Ref: status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}, Active: true,
			Details: list.Details{Secondary: "/shell"}},
		{Ref: status.PaneRef{Server: "server", ID: "%2", Generation: "birth"},
			Details: list.Details{Secondary: "/agent", Trailing: "opencode", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "opencode"}}}},
		{Ref: status.PaneRef{Server: "server", ID: "%3", Generation: "birth"},
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
		{"idle falls back to active shell", [3]status.State{status.Unreported, status.Idle, status.Idle}, 2, 0},
		{"unknown retains its own command", [3]status.State{status.Idle, status.Unknown, status.Idle}, 0, 1},
		{"withdrawal returns to active shell", [3]status.State{status.Idle, status.Withdrawn, status.Idle}, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, panes := windowStatusFixture(t)
			m.paneStatus.representatives = map[list.ItemID]status.PaneRef{id: panes[tc.previous].Ref}
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

func TestTreeStatusExpiryReprojectsFilteredRows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		m, panes := windowStatusFixture(t)
		values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
			panes[0].Ref: {State: status.Idle, ValidUntil: now.Add(time.Minute)},
			panes[1].Ref: {State: status.Unknown, Outcome: status.Completed, OutcomeSince: now, ValidUntil: now.Add(time.Minute)},
		})
		require.NoError(t, err)
		m.applyStatusUpdate(StatusUpdate{Values: values})
		m.applyStatusPresentation(now)
		m.applyScope("opencode", now)
		require.Len(t, m.items.Rows(), 2)
		m.applyScope("no-match", now)
		require.Empty(t, m.items.Rows())
		due := m.nextStatusFrame(now)
		require.Equal(t, now.Add(statusSuccessDuration), due)

		time.Sleep(statusSuccessDuration)
		next, cmd := m.update(statusFrameMsg{generation: m.paneStatus.frameGeneration})
		m = next.(model)
		m.setQuery("")

		require.Nil(t, cmd, "expiry is local, with no backend/status read")
		require.Empty(t, m.items.Rows()[1].Item.Trailing)
		require.Equal(t, "/shell", m.items.Rows()[1].Item.Secondary)
		require.Equal(t, status.Idle, m.paneStatus.rowValue(list.ItemID{ID: "window"}, time.Now()).State)
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
	require.Equal(t, panes[1].Ref, m.paneStatus.representatives[id])
	tree := m.items.Snapshot()
	reversed := slices.Clone(panes)
	slices.Reverse(reversed)
	reversed[1].Details.Trailing = "renamed-agent"
	updated := StatusUpdate{Values: values, Windows: map[list.ItemID][]WindowPane{id: reversed}}

	next, _ := m.handleActionResult(actionResultMsg{result: ActionResult{Snapshot: &tree, Status: &updated}})
	m = next.(model)

	require.Equal(t, panes[1].Ref, m.paneStatus.representatives[id], "reordered equal-priority panes keep their representative")
	require.Equal(t, "renamed-agent", m.items.Rows()[1].Item.Trailing)
	m.items.CollapseAll()
	m.applyScope("", now)
	require.Len(t, m.items.Rows(), 1, "status presentation preserves collapsed windows")
	flat := list.Snapshot{Items: []list.Item{{ID: list.ItemID{ID: "pane"}, Primary: "flat"}}}
	next, _ = m.handleActionResult(actionResultMsg{result: ActionResult{Snapshot: &flat}})
	m = next.(model)
	next, _ = m.handleActionResult(actionResultMsg{result: ActionResult{Snapshot: &tree, Status: &updated}})
	m = next.(model)

	require.Equal(t, panes[1].Ref, m.paneStatus.representatives[id])
	require.Equal(t, status.Working, m.paneStatus.rowValue(id, now).State)
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
