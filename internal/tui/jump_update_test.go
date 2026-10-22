package tui

import (
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func flatJumpSnapshot(count int) list.Snapshot {
	items := make([]list.Item, count)
	for i := range items {
		name := fmt.Sprintf("item %02d", i)
		items[i] = list.Item{
			ID: list.ItemID{ID: fmt.Sprintf("item-%02d", i)}, Primary: name,
			SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: name}},
		}
	}
	return list.Snapshot{Items: items}
}

func candidateIDs(jump jumpState) []list.ItemID {
	ids := make([]list.ItemID, len(jump.candidates))
	for i, candidate := range jump.candidates {
		ids[i] = candidate.itemID
	}
	return ids
}

func visibleIDs(m model) []list.ItemID {
	rows := m.items.VisibleRows()
	ids := make([]list.ItemID, len(rows))
	for i, row := range rows {
		ids[i] = row.Item.ID
	}
	return ids
}

func TestSemicolonIsFilterTextThenLabelsThePreservedResult(t *testing.T) {
	snapshot := list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "semicolon"}, Primary: "semi;colon",
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "semi;colon"}},
	}}}
	m := newModel(snapshot, nil)
	m, _ = updateModel(t, m, printableKey(";"))
	require.Equal(t, modeFilter, m.mode)
	require.Equal(t, ";", m.items.Query())
	m, _ = updateModel(t, m, specialKey(tea.KeyEscape))
	require.Equal(t, modeBrowse, m.mode)
	m, _ = updateModel(t, m, printableKey(";"))
	require.Equal(t, modeJump, m.mode)
	require.Equal(t, ";", m.items.Query())
	require.Equal(t, []list.ItemID{{ID: "semicolon"}}, candidateIDs(m.jump))
}

func TestJumpLabelsSessionWindowAndFilteredTreeRows(t *testing.T) {
	m := browseModel(newModel(interactionSnapshot(), nil))
	m, _ = updateModel(t, m, printableKey(";"))
	require.Equal(t, []list.ItemID{{ID: "session-1"}, {ID: "window-1"}, {ID: "window-2"}, {ID: "session-2"}, {ID: "window-3"}}, candidateIDs(m.jump))

	m.cancelJump()
	m.items.SetQuery("server")
	m, _ = updateModel(t, m, printableKey(";"))
	require.Equal(t, []list.ItemID{{ID: "session-1"}, {ID: "window-2"}}, candidateIDs(m.jump))
}

func TestJumpFreezesOnlyVisibleViewportCandidatesAndEscapeRestoresState(t *testing.T) {
	m := newModel(flatJumpSnapshot(40), nil)
	m.width, m.height = 80, 10
	m = m.reflow()
	m.items.SetQuery("item")
	m.items.Bottom()
	m = browseModel(m)
	beforeQuery := m.items.Query()
	beforeCursor, beforeOffset := m.items.Cursor(), m.items.Offset()
	beforeVisible := visibleIDs(m)
	require.NotZero(t, beforeOffset)

	m, _ = updateModel(t, m, printableKey(";"))
	require.Equal(t, modeJump, m.mode)
	require.Equal(t, beforeVisible, candidateIDs(m.jump))
	m, _ = updateModel(t, m, specialKey(tea.KeyEscape))
	require.Equal(t, modeBrowse, m.mode)
	require.Equal(t, beforeQuery, m.items.Query())
	require.Equal(t, beforeCursor, m.items.Cursor())
	require.Equal(t, beforeOffset, m.items.Offset())
}

func TestJumpControlNavigationStaysWithinFrozenCandidates(t *testing.T) {
	m := newModel(flatJumpSnapshot(40), nil)
	m.width, m.height = 80, 10
	m = browseModel(m.reflow())
	m, _ = updateModel(t, m, printableKey(";"))
	frozen := candidateIDs(m.jump)
	require.Greater(t, len(frozen), 2)

	m.jump.input = "a"
	m, _ = updateModel(t, m, controlKey('n'))
	require.Empty(t, m.jump.input)
	require.Equal(t, frozen[1], selectedNodeID(m))
	m, _ = updateModel(t, m, controlKey('p'))
	require.Equal(t, frozen[0], selectedNodeID(m))
	m, _ = updateModel(t, m, controlKey('p'))
	require.Equal(t, frozen[0], selectedNodeID(m))
	require.Equal(t, frozen, candidateIDs(m.jump))
	require.Equal(t, modeJump, m.mode)
}

func TestJumpDispatchAndNavigationKeepStructuralIdentity(t *testing.T) {
	first := list.ItemID{Scope: "first scope", Parent: "parent", ID: "shared"}
	second := list.ItemID{Scope: "second scope", Parent: "parent", ID: "shared"}
	var request ActionRequest
	m, err := newModelWithStartMode(list.Snapshot{Items: []list.Item{
		{ID: first, Primary: "first row"},
		{ID: second, Primary: "second row"},
	}}, func(value ActionRequest) (ActionResult, error) {
		request = value
		return ActionResult{Navigation: value.ItemID}, nil
	}, DefaultKeyMap(), StartModeNormal)
	require.NoError(t, err)
	m, _ = updateModel(t, m, printableKey(";"))
	require.Contains(t, terminal.Sanitize(m.View().Content), "a first row")
	require.Contains(t, terminal.Sanitize(m.View().Content), "s second row")

	m, dispatch := updateModel(t, m, printableKey("s"))
	require.NotNil(t, dispatch)
	m, quit := updateModel(t, m, dispatch())

	require.Equal(t, ActionRequest{ActionID: ActionOpen, ItemID: second}, request)
	require.Equal(t, second, m.navigation)
	requireQuit(t, quit)
}

func TestJumpRoutesShortcutLabelsBeforeNormalActions(t *testing.T) {
	for _, test := range []struct {
		label string
		item  list.ItemID
	}{
		{label: ";", item: list.ItemID{ID: "item-09"}},
		{label: "q", item: list.ItemID{ID: "item-10"}},
		{label: "n", item: list.ItemID{ID: "item-25"}},
	} {
		t.Run(test.label, func(t *testing.T) {
			var got ActionRequest
			m := newModel(flatJumpSnapshot(26), func(request ActionRequest) (ActionResult, error) {
				got = request
				return ActionResult{}, nil
			})
			m.width, m.height = 100, 40
			m = browseModel(m.reflow())
			m, _ = updateModel(t, m, printableKey(";"))

			m, cmd := updateModel(t, m, printableKey(test.label))
			require.Equal(t, modeBrowse, m.mode)
			require.True(t, m.busy)
			require.NotNil(t, cmd)
			cmd()
			require.Equal(t, ActionRequest{ActionID: ActionOpen, ItemID: test.item}, got)
		})
	}
}

func TestJumpAcceptsPartialLabelsAndReportsInvalidInput(t *testing.T) {
	var got ActionRequest
	m := newModel(flatJumpSnapshot(28), func(request ActionRequest) (ActionResult, error) {
		got = request
		return ActionResult{}, nil
	})
	m.width, m.height = 100, 40
	m = browseModel(m.reflow())
	m, _ = updateModel(t, m, printableKey(";"))
	require.Len(t, []rune(m.jump.candidates[0].label), 2)

	m, cmd := updateModel(t, m, printableKey("a"))
	require.Nil(t, cmd)
	require.Equal(t, modeJump, m.mode)
	require.Equal(t, "a", m.jump.input)
	_, cmd = updateModel(t, m, printableKey("a"))
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, ActionRequest{ActionID: ActionOpen, ItemID: list.ItemID{ID: "item-00"}}, got)

	m = newModel(flatJumpSnapshot(28), nil)
	m.width, m.height = 100, 40
	m = browseModel(m.reflow())
	m, _ = updateModel(t, m, printableKey(";"))
	m, cmd = updateModel(t, m, printableKey("!"))
	require.Nil(t, cmd)
	require.Equal(t, modeJump, m.mode)
	require.ErrorContains(t, m.err, "invalid jump label")
	require.Empty(t, m.jump.input)
}

func TestResizeAndSnapshotReplacementCancelJumpWithoutRetargeting(t *testing.T) {
	m := newModel(flatJumpSnapshot(28), nil)
	m.width, m.height = 100, 40
	m = browseModel(m.reflow())
	m, _ = updateModel(t, m, printableKey(";"))
	m, _ = updateModel(t, m, printableKey("a"))
	require.Equal(t, "a", m.jump.input)

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	require.Equal(t, modeBrowse, m.mode)
	require.Empty(t, m.jump.candidates)

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 90, Height: 40})
	m.items.SetQuery("item")
	m.items.Bottom()
	beforeQuery := m.items.Query()
	beforeCursor, beforeOffset := m.items.Cursor(), m.items.Offset()
	m, _ = updateModel(t, m, printableKey(";"))
	m, _ = updateModel(t, m, printableKey("a"))
	require.Equal(t, "a", m.jump.input)
	next := flatJumpSnapshot(28)
	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &next}})
	require.Equal(t, modeBrowse, m.mode)
	require.Empty(t, m.jump.candidates)
	require.Equal(t, beforeQuery, m.items.Query())
	require.Equal(t, beforeCursor, m.items.Cursor())
	require.Equal(t, beforeOffset, m.items.Offset())
}

func TestJumpStaleTargetFailsLoudlyWithoutReplacingTheList(t *testing.T) {
	m := newModel(flatJumpSnapshot(2), func(ActionRequest) (ActionResult, error) {
		return ActionResult{}, errors.New("selected item is stale")
	})
	before := m.items.Snapshot()
	beforeCursor, beforeOffset := m.items.Cursor(), m.items.Offset()
	m = browseModel(m)
	m, _ = updateModel(t, m, printableKey(";"))
	m, cmd := updateModel(t, m, printableKey("s"))
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())

	require.Equal(t, modeBrowse, m.mode)
	require.ErrorContains(t, m.err, "stale")
	require.Equal(t, before, m.items.Snapshot())
	require.Equal(t, beforeCursor, m.items.Cursor())
	require.Equal(t, beforeOffset, m.items.Offset())
	require.Equal(t, list.ItemID{ID: "item-00"}, selectedNodeID(m))
	require.Empty(t, m.navigation)
}

func TestSlashFromJumpEntersFilterWithoutChangingTheQuery(t *testing.T) {
	m := newModel(flatJumpSnapshot(2), nil)
	m.items.SetQuery("item")
	m = browseModel(m)
	m, _ = updateModel(t, m, printableKey(";"))
	m, _ = updateModel(t, m, printableKey("/"))
	require.Equal(t, modeFilter, m.mode)
	require.Equal(t, "item", m.items.Query())
	require.Empty(t, m.jump.candidates)
}
