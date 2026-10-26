package tui

import (
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestWindowOpenCapturesTheDisplayedPane(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   uiMode
		key    tea.KeyPressMsg
		custom bool
	}{
		{name: "normal Enter", mode: modeBrowse, key: specialKey(tea.KeyEnter)},
		{name: "filter Enter", mode: modeFilter, key: specialKey(tea.KeyEnter)},
		{name: "menu Enter", mode: modeMenu, key: specialKey(tea.KeyEnter)},
		{name: "custom Open", mode: modeBrowse, key: controlKey('b'), custom: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, panes := windowStatusFixture(t)
				if tc.custom {
					keys, err := ResolveKeyMap(map[KeyMode][]Binding{
						KeyModeNormal: {{Action: ActionOpen, Keys: []string{"ctrl+b"}}},
					})
					require.NoError(t, err)
					m.keys = keys
				}
				id := list.ItemID{ID: "window"}
				values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
					panes[0].Ref: {State: status.Working, ValidUntil: time.Now().Add(time.Hour)},
					panes[1].Ref: {State: status.Blocked, ValidUntil: time.Now().Add(statusSuccessDuration)},
				})
				require.NoError(t, err)
				m, _ = updateModel(t, m, statusResultMsg{update: StatusUpdate{Values: values}})
				require.True(t, m.items.Select(id))
				m.mode = tc.mode
				if tc.mode == modeMenu {
					m.menu = menuState{ItemID: id, Entries: []MenuEntry{{Action: ActionOpen}}, ReturnMode: modeBrowse}
				}
				var request ActionRequest
				m.dispatch = func(got ActionRequest) (ActionResult, error) {
					request = got
					return ActionResult{Navigation: id}, nil
				}
				row, ok := m.items.Selected()
				require.True(t, ok)
				require.Equal(t, "/agent", row.Item.Secondary)
				require.Equal(t, "opencode", row.Item.Trailing)

				m, open := updateModel(t, m, tc.key)
				require.NotNil(t, open)
				time.Sleep(statusSuccessDuration)
				m, _ = updateModel(t, m, statusFrameMsg{generation: m.paneStatus.frameGeneration})
				row, ok = m.items.Selected()
				require.True(t, ok)
				require.Equal(t, "/shell", row.Item.Secondary, "expiry changes the displayed representative while Open is pending")
				result := open()
				m, _ = updateModel(t, m, result)

				require.Equal(t, ActionRequest{ActionID: ActionOpen, ItemID: id, PaneID: "%2"}, request)
				require.Equal(t, id, m.navigation)
			})
		})
	}
}

func TestWindowOpenUsesUntrackedDisplayedPaneWithoutChangingMutations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		key    string
		action ActionID
		pane   string
	}{
		{name: "open", key: "o", action: ActionOpen, pane: "%1"},
		{name: "rename window", key: "r", action: ActionRename},
		{name: "delete window", key: "x", action: ActionDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, panes := windowStatusFixture(t)
			panes[0].Ref = status.PaneRef{}
			panes[0].PaneID = "%1"
			m, _ = updateModel(t, m, statusResultMsg{update: StatusUpdate{Values: status.Snapshot{}}})
			id := list.ItemID{ID: "window"}
			m.mode = modeMenu
			m.menu = menuState{ItemID: id, Entries: []MenuEntry{{Action: tc.action}}, ReturnMode: modeBrowse}
			var request ActionRequest
			m.dispatch = func(got ActionRequest) (ActionResult, error) {
				request = got
				return ActionResult{}, nil
			}

			_, cmd := updateModel(t, m, printableKey(tc.key))
			require.NotNil(t, cmd)
			cmd()

			require.Equal(t, ActionRequest{ActionID: tc.action, ItemID: id, PaneID: tc.pane}, request)
		})
	}
}
