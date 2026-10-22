package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func previewModel(t *testing.T) model {
	t.Helper()
	snapshot := list.Snapshot{Items: []list.Item{
		{ID: list.ItemID{ID: "a"}, Primary: "alpha", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "alpha"}}},
		{ID: list.ItemID{ID: "b"}, Primary: "bravo", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "bravo"}}},
		{ID: list.ItemID{ID: "c"}, Primary: "charlie", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "charlie"}}},
	}}
	m, err := newModelWithStartMode(snapshot, nil, DefaultKeyMap(), StartModeNormal)
	require.NoError(t, err)
	m.width, m.height = 120, 12
	screens := map[list.ItemID]string{
		{ID: "a"}: "screen a",
		{ID: "b"}: "screen b",
		{ID: "c"}: "screen c",
	}
	m.readPreview = func(_ context.Context, id list.ItemID) (string, error) {
		screen, exists := screens[id]
		if !exists {
			return "", fmt.Errorf("unexpected preview item %q", id)
		}
		return screen, nil
	}
	return m.reflow()
}

func previewKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt} }

func TestPreviewToggleModeOwnership(t *testing.T) {
	for _, mode := range []uiMode{modeBrowse, modeFilter, modeJump, modeInput, modeConfirm, modeMenu} {
		t.Run(string(mode), func(t *testing.T) {
			m := previewModel(t)
			if mode == modeJump {
				m, _ = updateModel(t, m, printableKey(";"))
			} else {
				m.mode = mode
			}
			candidates, query, selected := m.jump.candidates, m.items.Query(), selectedNodeID(m)
			m, cmd := updateModel(t, m, previewKey())
			want := mode == modeBrowse || mode == modeFilter || mode == modeJump
			require.Equal(t, want, m.preview.visible)
			require.Equal(t, mode, m.mode)
			require.Equal(t, query, m.items.Query())
			require.Equal(t, selected, selectedNodeID(m))
			require.Equal(t, candidates, m.jump.candidates)
			require.False(t, m.busy)
			if want {
				require.NotNil(t, cmd)
				m, cmd = updateModel(t, m, cmd())
				require.Nil(t, cmd)
				require.True(t, m.preview.ready)
				m, cmd = updateModel(t, m, previewKey())
				require.False(t, m.preview.visible)
				require.Nil(t, cmd)
				require.Empty(t, m.preview.lines)
			} else {
				require.Nil(t, cmd)
			}
		})
	}
	m := previewModel(t)
	m.busy = true
	m, cmd := updateModel(t, m, previewKey())
	require.False(t, m.preview.visible)
	require.Nil(t, cmd)
}

func TestPreviewReflowsStackedFlatList(t *testing.T) {
	for _, mode := range []uiMode{modeBrowse, modeFilter, modeJump} {
		t.Run(string(mode), func(t *testing.T) {
			snapshot := flatJumpSnapshot(40)
			for i := range snapshot.Items {
				snapshot.Items[i].Context = "hetki"
			}
			m := browseModel(newModel(snapshot, nil))
			m.width, m.height = 100, 24
			m = m.reflow()
			m.items.Select(snapshot.Items[15].ID)
			m.mode = mode
			if mode == modeJump {
				m.jump = newJumpState(m.items.VisibleRows())
			}

			m, _ = updateModel(t, m, previewKey())

			require.Equal(t, 10, m.items.Height())
			require.Len(t, m.items.VisibleRows(), 10)
			require.Equal(t, snapshot.Items[15].ID, selectedNodeID(m))
			require.Contains(t, visibleIDs(m), selectedNodeID(m))
			require.LessOrEqual(t, lipgloss.Height(m.View().Content), m.height)
			require.Empty(t, m.jump.candidates)
			require.NotEqual(t, modeJump, m.mode)

			m, _ = updateModel(t, m, specialKey(tea.KeyPgDown))

			require.Equal(t, snapshot.Items[25].ID, selectedNodeID(m))
			require.Contains(t, visibleIDs(m), selectedNodeID(m))

			m, _ = updateModel(t, m, previewKey())

			require.Equal(t, 20, m.items.Height())
			require.Len(t, m.items.VisibleRows(), 20)
			require.Equal(t, snapshot.Items[25].ID, selectedNodeID(m))
			require.LessOrEqual(t, lipgloss.Height(m.View().Content), m.height)
		})
	}
}

func TestPreviewDistinguishesEqualLocalIDs(t *testing.T) {
	first := list.ItemID{Scope: "first scope", Parent: "parent", ID: "shared"}
	second := list.ItemID{Scope: "second scope", Parent: "parent", ID: "shared"}
	third := list.ItemID{Scope: "second scope", Parent: "other parent", ID: "shared"}
	m, err := newModelWithStartMode(list.Snapshot{Items: []list.Item{
		{ID: first, Primary: "first row"},
		{ID: second, Primary: "second row"},
		{ID: third, Primary: "third row"},
	}}, nil, DefaultKeyMap(), StartModeNormal)
	require.NoError(t, err)
	var captured []list.ItemID
	m.readPreview = func(_ context.Context, id list.ItemID) (string, error) {
		captured = append(captured, id)
		return "captured screen", nil
	}

	for _, key := range []tea.KeyPressMsg{previewKey(), specialKey(tea.KeyDown), specialKey(tea.KeyDown)} {
		var cmd tea.Cmd
		m, cmd = updateModel(t, m, key)
		require.NotNil(t, cmd)
		m, _ = updateModel(t, m, cmd())
	}

	require.Equal(t, []list.ItemID{first, second, third}, captured)
	require.Contains(t, terminal.Sanitize(m.View().Content), "captured screen")
}

func TestPreviewHasNoHiddenOrRepeatingRead(t *testing.T) {
	m := previewModel(t)
	calls := 0
	read := m.readPreview
	m.readPreview = func(ctx context.Context, id list.ItemID) (string, error) {
		calls++
		return read(ctx, id)
	}
	require.Nil(t, m.Init())
	for _, key := range []tea.KeyPressMsg{specialKey(tea.KeyDown), specialKey(tea.KeyUp)} {
		var cmd tea.Cmd
		m, cmd = updateModel(t, m, key)
		require.Nil(t, cmd)
	}
	require.Zero(t, calls)
	m, cmd := updateModel(t, m, previewKey())
	m, cmd = updateModel(t, m, cmd())
	require.Nil(t, cmd, "snapshot completion must not schedule polling")
	for range 5 {
		m, cmd = updateModel(t, m, nil)
		require.Nil(t, cmd)
	}
	m, cmd = updateModel(t, m, tea.WindowSizeMsg{Width: 130, Height: 14})
	require.Nil(t, cmd, "ordinary resize only crops the existing snapshot")
	require.Equal(t, 1, calls)
	m, cmd = updateModel(t, m, specialKey(tea.KeyDown))
	require.NotNil(t, cmd)
	m, cmd = updateModel(t, m, cmd())
	require.Nil(t, cmd)
	require.Equal(t, 2, calls)
	require.Equal(t, "screen b", terminal.Sanitize(m.preview.lines[0]))
}

func TestPreviewCoalescesSelectionAndRejectsStaleResults(t *testing.T) {
	m := previewModel(t)
	var canceled error
	read := m.readPreview
	m.readPreview = func(ctx context.Context, id list.ItemID) (string, error) {
		canceled = ctx.Err()
		return read(ctx, id) // Simulate a completed read racing cancellation.
	}
	m, old := updateModel(t, m, previewKey())
	m, cmd := updateModel(t, m, specialKey(tea.KeyDown))
	require.Nil(t, cmd, "one read at a time")
	m, cmd = updateModel(t, m, specialKey(tea.KeyDown))
	require.Nil(t, cmd)
	require.Empty(t, m.preview.lines)
	m, cmd = updateModel(t, m, old())
	require.ErrorIs(t, canceled, context.Canceled)
	require.Empty(t, m.preview.lines, "stale content must never flash under the new selection")
	require.NotNil(t, cmd, "capture only the latest selection")
	m, cmd = updateModel(t, m, cmd())
	require.Nil(t, cmd)
	require.Equal(t, "screen c", terminal.Sanitize(m.preview.lines[0]))
	require.NoError(t, canceled)
}

func TestPreviewReopenRejectsPreviousGenerationOfSameItem(t *testing.T) {
	m := previewModel(t)
	m, old := updateModel(t, m, previewKey())
	m, _ = updateModel(t, m, previewKey())
	m, cmd := updateModel(t, m, previewKey())
	require.Nil(t, cmd, "wait for the canceled read before starting another")
	m, cmd = updateModel(t, m, old())
	require.False(t, m.preview.ready)
	require.Empty(t, m.preview.lines)
	require.NotNil(t, cmd, "reopening must recapture even the same pane")
}

func TestPreviewFilteringErrorsAndEmptySelection(t *testing.T) {
	m := previewModel(t)
	m.readPreview = func(context.Context, list.ItemID) (string, error) {
		return "", errors.New("missing pane\x1b]52;c;secret\a\nnext")
	}
	m, cmd := updateModel(t, m, previewKey())
	m, cmd = updateModel(t, m, cmd())
	require.Nil(t, cmd, "errors do not retry automatically")
	require.NoError(t, m.err, "preview failure must not overwrite the action error")
	require.Contains(t, terminal.Sanitize(m.View().Content), "missing pane")
	require.NotContains(t, m.View().Content, "secret")
	m, _ = updateModel(t, m, printableKey("/"))
	m, cmd = updateModel(t, m, tea.PasteMsg{Content: "bravo"})
	require.NotNil(t, cmd)
	require.Equal(t, list.ItemID{ID: "b"}, m.preview.itemID)
	result := cmd()
	m, _ = updateModel(t, m, tea.PasteMsg{Content: "no match"})
	m, cmd = updateModel(t, m, result)
	require.Nil(t, cmd)
	require.Empty(t, m.preview.itemID)
	require.Empty(t, m.preview.lines)
	require.Contains(t, terminal.Sanitize(m.View().Content), "no selection")
}

func TestPreviewRefreshInvalidatesUnchangedItem(t *testing.T) {
	m := previewModel(t)
	snapshot := m.items.Snapshot()
	m.dispatch = func(ActionRequest) (ActionResult, error) { return ActionResult{Snapshot: &snapshot}, nil }
	m, cmd := updateModel(t, m, previewKey())
	m, _ = updateModel(t, m, cmd())
	m, cmd = updateModel(t, m, controlKey('r'))
	require.True(t, m.busy)
	require.NotEmpty(t, m.preview.lines, "keep the snapshot until refresh succeeds")
	m, cmd = updateModel(t, m, cmd())
	require.Empty(t, m.preview.lines)
	require.False(t, m.busy)
	require.NotNil(t, cmd, "same item ID can resolve to a different pane after refresh")
	m, cmd = updateModel(t, m, cmd())
	require.Nil(t, cmd)
	require.Equal(t, list.ItemID{ID: "a"}, m.preview.itemID)
}

func TestPreviewRetainsSnapshotAcrossReadOnlyOverlays(t *testing.T) {
	for _, result := range []ActionResult{
		{Menu: &ItemMenu{Title: "ACTIONS", Entries: []MenuEntry{{Action: ActionOpen, Label: "Open"}}}},
		{Input: &InputPrompt{Title: "Rename"}},
		{Confirmation: &Confirmation{Title: "Delete?"}},
	} {
		m := previewModel(t)
		m.dispatch = func(ActionRequest) (ActionResult, error) { return result, nil }
		m, cmd := updateModel(t, m, previewKey())
		m, _ = updateModel(t, m, cmd())
		generation, lines := m.preview.generation, m.preview.lines
		m, cmd = updateModel(t, m, controlKey('k'))
		require.NotEmpty(t, m.preview.lines)
		m, cmd = updateModel(t, m, cmd())
		require.Nil(t, cmd, "opening an overlay must not recapture")
		m, cmd = updateModel(t, m, specialKey(tea.KeyEscape))
		require.Nil(t, cmd, "canceling an overlay must not recapture")
		require.Equal(t, generation, m.preview.generation)
		require.Equal(t, lines, m.preview.lines)
	}
}

func TestPreviewCancellationOnHideResizeAndQuit(t *testing.T) {
	for _, msg := range []tea.Msg{previewKey(), tea.WindowSizeMsg{Width: 10, Height: 12}, controlKey('c'), printableKey("q")} {
		m := previewModel(t)
		var readCtx context.Context
		m.readPreview = func(ctx context.Context, _ list.ItemID) (string, error) {
			readCtx = ctx //nolint:fatcontext // records the context to assert its cancellation
			return "ignored", nil
		}
		m, pending := updateModel(t, m, previewKey())
		m, _ = updateModel(t, m, msg)
		m, cmd := updateModel(t, m, pending())
		require.ErrorIs(t, readCtx.Err(), context.Canceled)
		require.Nil(t, cmd)
		require.Empty(t, m.preview.lines)
	}
}

func TestPreviewShowsBottomRowsWithoutTitle(t *testing.T) {
	m := previewModel(t)
	m.preview.itemID, m.preview.ready = list.ItemID{ID: "a"}, true
	m.preview.lines = terminal.ScreenLines("first\nsecond\nthird\n")
	require.Equal(t, "second\\nthird ", terminal.Sanitize(m.viewPreview(6, 2)))
	require.Equal(t, "third ", terminal.Sanitize(m.viewPreview(6, 1)))
}

func TestPreviewEndsAtLastNonblankRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		capture string
		want    string
	}{
		{"empty footer", "first\nsecond\nthird\n\n   \n\t\n", "second\\nthird "},
		{"styled empty footer", "first\nsecond\n\x1b[32mthird\x1b[0m\n\x1b[41m   \x1b[0m\n\n", "second\\nthird "},
		{"internal blank row", "first\n\nthird\n\n", "      \\nthird "},
		{"all blank", "\n   \n\x1b[41m \x1b[0m\n", "      \\n      "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := previewModel(t)
			m.preview.itemID = list.ItemID{ID: "a"}

			updated, _ := m.handlePreviewResult(previewResultMsg{generation: m.preview.generation, text: tc.capture})
			m = updated.(model)

			require.Equal(t, tc.want, terminal.Sanitize(m.viewPreview(6, 2)))
		})
	}
}

func TestPreviewLayoutFitsAndKeepsListHeight(t *testing.T) {
	for _, width := range []int{1, 2, 3, 20, 48, 60, 120, 200} {
		for _, height := range []int{1, 4, 12} {
			for _, percent := range []int{1, 50, DefaultPreviewWidth, 99} {
				m := previewModel(t)
				m.width, m.height, m.preview.width = width, height, percent
				m = m.reflow()
				listHeight := m.items.Height()
				m.readPreview = func(context.Context, list.ItemID) (string, error) {
					return strings.Repeat("\x1b[41m中👨‍👩‍👧é "+strings.Repeat("x", 300)+"\n", 100), nil
				}
				m, cmd := updateModel(t, m, previewKey())
				if cmd != nil {
					m, _ = updateModel(t, m, cmd())
				}
				require.Equal(t, listHeight, m.items.Height())
				layout := m.layout()
				if layout.previewWidth > 0 {
					require.Equal(t, layout.innerWidth, layout.listWidth+3+layout.previewWidth)
					require.GreaterOrEqual(t, layout.previewWidth, 20)
					require.GreaterOrEqual(t, layout.listWidth, 20)
				} else {
					require.Nil(t, cmd)
				}
				content := m.View().Content
				require.LessOrEqual(t, lipgloss.Height(content), max(3, height))
				for _, line := range strings.Split(content, "\n") {
					require.LessOrEqual(t, terminal.Width(line), width, "width %d height %d: %q", width, height, line)
				}
			}
		}
	}
}
