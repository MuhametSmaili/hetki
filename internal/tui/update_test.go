package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func interactionSnapshot() list.Snapshot {
	return list.Snapshot{ActiveItemID: list.ItemID{ID: "window-1"}, Items: []list.Item{
		{
			ID: list.ItemID{ID: "session-1"}, Primary: "dev", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "dev"}},
			Children: []list.Item{
				{ID: list.ItemID{ID: "window-1"}, Primary: "editor", Secondary: "~/code/editor", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "editor"}, {Tier: list.SearchSecondary, Text: "~/code/editor"}}},
				{ID: list.ItemID{ID: "window-2"}, Primary: "server", Secondary: "~/svc/api", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "server"}, {Tier: list.SearchSecondary, Text: "~/svc/api"}}},
			},
		},
		{
			ID: list.ItemID{ID: "session-2"}, Primary: "prod", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "prod"}},
			Children: []list.Item{{ID: list.ItemID{ID: "window-3"}, Primary: "shell", SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "shell"}}}},
		},
	}}
}

func printableKey(text string) tea.KeyPressMsg {
	runes := []rune(text)
	return tea.KeyPressMsg{Code: runes[0], Text: text}
}

func specialKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func controlKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl} }

func updateModel(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	result, ok := updated.(model)
	require.True(t, ok, "Update returned %T", updated)
	return result, cmd
}

func selectNode(t *testing.T, m model, id list.ItemID) model {
	t.Helper()
	require.True(t, m.items.Select(id), "item %q is not visible", id)
	return m
}

func browseModel(m model) model {
	m.mode = modeBrowse
	m.initialJump = false
	m.jump = jumpState{}
	return m
}

func selectedNodeID(m model) list.ItemID {
	if selected, ok := m.selectedRow(); ok {
		return selected.Item.ID
	}
	return list.ItemID{}
}

func requireQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	require.NotNil(t, cmd)
	_, ok := cmd().(tea.QuitMsg)
	require.True(t, ok, "command did not return tea.QuitMsg")
}

func TestUpdateRoutesKeysByModeAndOverlay(t *testing.T) {
	m := newModel(interactionSnapshot(), nil)
	require.Equal(t, modeFilter, m.mode)

	m, cmd := updateModel(t, m, printableKey("q"))
	require.Nil(t, cmd)
	require.Equal(t, "q", m.items.Query(), "filter must own printable shortcut keys")
	m, _ = updateModel(t, m, printableKey("?"))
	require.Equal(t, "q?", m.items.Query(), "removed help key must remain searchable text")
	m, _ = updateModel(t, m, specialKey(tea.KeyEscape))
	require.Equal(t, modeBrowse, m.mode)
	require.Equal(t, "q?", m.items.Query(), "escape must preserve the query")

	m.mode = modeInput
	m.input = inputState{Value: "name"}
	m, cmd = updateModel(t, m, printableKey("q"))
	require.Nil(t, cmd)
	require.Equal(t, "nameq", m.input.Value)
	m, cmd = updateModel(t, m, controlKey('k'))
	require.Nil(t, cmd)
	require.Equal(t, modeInput, m.mode, "input must own ctrl+k")
	m, cmd = updateModel(t, m, specialKey(tea.KeyTab))
	require.Nil(t, cmd)
	require.Equal(t, modeInput, m.mode, "tab must not change an input target")
	require.Equal(t, "nameq", m.input.Value)
	m, _ = updateModel(t, m, specialKey(tea.KeyEscape))
	require.Equal(t, modeBrowse, m.mode)

	m.mode = modeConfirm
	m.confirm = confirmState{Request: ActionRequest{ActionID: ActionDelete, ItemID: list.ItemID{ID: "session-1"}}}
	m, cmd = updateModel(t, m, printableKey("q"))
	require.Nil(t, cmd)
	require.Equal(t, modeConfirm, m.mode)
	m, cmd = updateModel(t, m, controlKey('k'))
	require.Nil(t, cmd)
	require.Equal(t, modeConfirm, m.mode, "confirmation must own ctrl+k")
	m, cmd = updateModel(t, m, specialKey(tea.KeyTab))
	require.Nil(t, cmd)
	require.Equal(t, modeConfirm, m.mode, "tab must not change a confirmation target")
	m, _ = updateModel(t, m, printableKey("n"))
	require.Equal(t, modeBrowse, m.mode)

	m.busy = true
	m, cmd = updateModel(t, m, controlKey('k'))
	require.Nil(t, cmd)
	require.True(t, m.busy, "busy state must own ctrl+k")
	_, cmd = updateModel(t, m, controlKey('c'))
	requireQuit(t, cmd)
}

func TestEnteringFilterPreservesExistingError(t *testing.T) {
	want := errors.New("stale target")
	m := browseModel(newModel(interactionSnapshot(), nil))
	m.err = want

	m, _ = updateModel(t, m, printableKey("/"))
	require.Equal(t, modeFilter, m.mode)
	require.ErrorIs(t, m.err, want)
}

func TestEnterOpensSelectedItemFromFilter(t *testing.T) {
	var got ActionRequest
	m := newModel(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
		got = request
		return ActionResult{}, nil
	})
	m.items.SetQuery("server")
	require.Equal(t, list.ItemID{ID: "window-2"}, selectedNodeID(m))

	m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))
	require.Equal(t, modeBrowse, m.mode)
	require.True(t, m.busy)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, ActionRequest{ActionID: ActionOpen, ItemID: list.ItemID{ID: "window-2"}}, got)
}

func TestLastSessionKeyWorksWithoutVisibleRowsAndRespectsOverlays(t *testing.T) {
	for _, test := range []struct {
		name string
		mode uiMode
		busy bool
	}{
		{name: "normal", mode: modeBrowse},
		{name: "filter", mode: modeFilter},
		{name: "jump", mode: modeJump},
		{name: "input", mode: modeInput},
		{name: "confirmation", mode: modeConfirm},
		{name: "menu", mode: modeMenu},
		{name: "busy", mode: modeBrowse, busy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			unavailable := errors.New("no previous session available")
			m := newModel(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
				require.Equal(t, ActionRequest{ActionID: ActionLastSession}, request)
				return ActionResult{}, unavailable
			})
			m.items.SetQuery("no-match")
			require.Empty(t, m.items.Rows())
			m.mode, m.busy = test.mode, test.busy
			m, cmd := updateModel(t, m, controlKey('o'))
			if test.busy || test.mode == modeInput || test.mode == modeConfirm || test.mode == modeMenu {
				require.Nil(t, cmd)
				return
			}
			require.NotNil(t, cmd)
			require.True(t, m.busy)
			m, cmd = updateModel(t, m, cmd())
			require.Nil(t, cmd)
			require.False(t, m.busy)
			require.ErrorIs(t, m.err, unavailable)
			require.Equal(t, test.mode, m.mode)
			require.Equal(t, "no-match", m.items.Query())
		})
	}
}

func TestDirectActionKeysEmitStableActionAndItemIDs(t *testing.T) {
	for _, test := range []struct {
		name       string
		key        tea.KeyPressMsg
		selectedID list.ItemID
		want       ActionRequest
	}{
		{name: "open", key: specialKey(tea.KeyEnter), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionOpen, ItemID: list.ItemID{ID: "window-2"}}},
		{name: "refresh", key: controlKey('r'), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionRefresh}},
		{name: "toggle projection", key: controlKey('t'), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionToggleProjection, ItemID: list.ItemID{ID: "window-2"}}},
		{name: "create session", key: printableKey("A"), selectedID: list.ItemID{ID: "session-1"}, want: ActionRequest{ActionID: ActionCreateSession}},
		{name: "create window", key: printableKey("a"), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionCreateWindow, ItemID: list.ItemID{ID: "window-2"}}},
		{name: "rename window", key: printableKey("r"), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionRename, ItemID: list.ItemID{ID: "window-2"}}},
		{name: "rename session", key: printableKey("R"), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionRenameSession, ItemID: list.ItemID{ID: "window-2"}}},
		{name: "delete window", key: printableKey("x"), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionDelete, ItemID: list.ItemID{ID: "window-2"}}},
		{name: "delete session", key: printableKey("X"), selectedID: list.ItemID{ID: "window-2"}, want: ActionRequest{ActionID: ActionDeleteSession, ItemID: list.ItemID{ID: "window-2"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got ActionRequest
			m := newModel(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
				got = request
				return ActionResult{}, nil
			})
			m = browseModel(m)
			m = selectNode(t, m, test.selectedID)
			m, cmd := updateModel(t, m, test.key)
			require.True(t, m.busy)
			require.NotNil(t, cmd)
			cmd()
			require.Equal(t, test.want, got)
		})
	}
}

func TestBoundDeleteSessionUsesOneActionFlowInEveryMode(t *testing.T) {
	for _, test := range []struct {
		name       string
		mode       uiMode
		keyMode    KeyMode
		returnMode uiMode
	}{
		{name: "normal", mode: modeBrowse, keyMode: KeyModeNormal, returnMode: modeBrowse},
		{name: "jump", mode: modeJump, keyMode: KeyModeJump, returnMode: modeBrowse},
		{name: "filter", mode: modeFilter, keyMode: KeyModeFilter, returnMode: modeFilter},
		{name: "input", mode: modeInput, keyMode: KeyModeInput, returnMode: modeFilter},
		{name: "confirm", mode: modeConfirm, keyMode: KeyModeConfirm, returnMode: modeFilter},
		{name: "menu", mode: modeMenu, keyMode: KeyModeMenu, returnMode: modeFilter},
	} {
		t.Run(test.name, func(t *testing.T) {
			bindings := map[KeyMode][]Binding{
				test.keyMode: {{Action: ActionDeleteSession, Keys: []string{"z"}}},
			}
			if test.keyMode == KeyModeConfirm {
				bindings[KeyModeConfirm] = append(bindings[KeyModeConfirm], Binding{Action: ActionConfirm, Keys: []string{"y"}})
			} else {
				bindings[KeyModeConfirm] = []Binding{{Action: ActionConfirm, Keys: []string{"y"}}}
			}
			keys, err := ResolveKeyMap(bindings)
			require.NoError(t, err)

			var requests []ActionRequest
			m, err := newModelWithKeys(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
				requests = append(requests, request)
				if !request.Confirmed {
					return ActionResult{Confirmation: &Confirmation{Title: "DELETE", Body: "Delete session?"}}, nil
				}
				return ActionResult{}, nil
			}, keys)
			require.NoError(t, err)
			m = selectNode(t, browseModel(m), list.ItemID{ID: "window-2"})
			m.mode = test.mode
			m.input = inputState{Value: "name", ReturnMode: test.returnMode}
			m.confirm = confirmState{ReturnMode: test.returnMode}
			m.menu = menuState{ItemID: list.ItemID{ID: "window-2"}, Entries: []MenuEntry{{Action: ActionDeleteSession, Label: "Delete session"}}, ReturnMode: test.returnMode}

			m, cmd := updateModel(t, m, printableKey("z"))
			require.NotNil(t, cmd)
			m, _ = updateModel(t, m, cmd())
			require.Equal(t, modeConfirm, m.mode)
			require.Equal(t, test.returnMode, m.confirm.ReturnMode)
			require.Equal(t, []ActionRequest{{ActionID: ActionDeleteSession, ItemID: list.ItemID{ID: "window-2"}}}, requests)

			m, cmd = updateModel(t, m, printableKey("y"))
			require.NotNil(t, cmd)
			require.Equal(t, test.returnMode, m.mode)
			cmd()
			require.Equal(t, []ActionRequest{
				{ActionID: ActionDeleteSession, ItemID: list.ItemID{ID: "window-2"}},
				{ActionID: ActionDeleteSession, ItemID: list.ItemID{ID: "window-2"}, Confirmed: true},
			}, requests)
		})
	}
}

func TestActionMnemonicsRemainTextInFilterAndInput(t *testing.T) {
	for _, mode := range []uiMode{modeFilter, modeInput} {
		m := browseModel(newModel(interactionSnapshot(), nil))
		m.mode = mode
		for _, letter := range "rRxXaA" {
			var cmd tea.Cmd
			m, cmd = updateModel(t, m, printableKey(string(letter)))
			require.Nil(t, cmd)
			require.False(t, m.busy)
		}
		if mode == modeFilter {
			require.Equal(t, "rRxXaA", m.items.Query())
		} else {
			require.Equal(t, "rRxXaA", m.input.Value)
		}
	}
}

func TestProjectionTogglePreservesTheFilterAndUsesPreferredSelection(t *testing.T) {
	flat := list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "destination-2"}, Primary: "beta > api",
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "server"}},
	}}}
	m := newModel(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
		require.Equal(t, ActionRequest{ActionID: ActionToggleProjection, ItemID: list.ItemID{ID: "window-2"}}, request)
		return ActionResult{Snapshot: &flat, SelectItemID: list.ItemID{ID: "destination-2"}}, nil
	})
	m = browseModel(m)
	m = selectNode(t, m, list.ItemID{ID: "window-2"})
	m.items.SetQuery("server")

	m, cmd := updateModel(t, m, controlKey('t'))
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())
	require.Equal(t, "server", m.items.Query())
	require.Equal(t, list.ItemID{ID: "destination-2"}, selectedNodeID(m))
}

func TestCtrlTFromJumpTogglesProjectionAndRebuildsLabels(t *testing.T) {
	flat := list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{ID: "destination-2"}, Primary: "beta > api",
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: "server"}},
	}}}
	m := newModel(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
		require.Equal(t, ActionRequest{ActionID: ActionToggleProjection, ItemID: list.ItemID{ID: "window-2"}}, request)
		return ActionResult{Snapshot: &flat, SelectItemID: list.ItemID{ID: "destination-2"}}, nil
	})
	m = browseModel(m)
	m = selectNode(t, m, list.ItemID{ID: "window-2"})
	m, _ = updateModel(t, m, printableKey(";"))
	require.Equal(t, modeJump, m.mode)

	m, cmd := updateModel(t, m, controlKey('t'))
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())
	require.Equal(t, modeJump, m.mode)
	require.Equal(t, []jumpCandidate{{label: "a", itemID: list.ItemID{ID: "destination-2"}}}, m.jump.candidates)
	require.Equal(t, list.ItemID{ID: "destination-2"}, selectedNodeID(m))
}

func TestPromptAndConfirmationKeepTheOriginatingItemID(t *testing.T) {
	var requests []ActionRequest
	m := newModel(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
		requests = append(requests, request)
		if request.ActionID == ActionRename && request.Value == nil {
			return ActionResult{Input: &InputPrompt{Title: "RENAME", Prompt: "Name"}}, nil
		}
		if request.ActionID == ActionDelete && !request.Confirmed {
			return ActionResult{Confirmation: &Confirmation{Title: "DELETE", Body: "Delete?"}}, nil
		}
		return ActionResult{}, nil
	})
	m = browseModel(m)
	m = selectNode(t, m, list.ItemID{ID: "window-2"})

	m, cmd := updateModel(t, m, printableKey("r"))
	m, _ = updateModel(t, m, cmd())
	require.Equal(t, modeInput, m.mode)
	require.Equal(t, list.ItemID{ID: "window-2"}, m.input.Request.ItemID)
	m.input.Value = "api"
	m, cmd = updateModel(t, m, specialKey(tea.KeyEnter))
	m, _ = updateModel(t, m, cmd())
	require.Equal(t, list.ItemID{ID: "window-2"}, requests[len(requests)-1].ItemID)
	require.Equal(t, "api", *requests[len(requests)-1].Value)

	m, cmd = updateModel(t, m, printableKey("x"))
	m, _ = updateModel(t, m, cmd())
	require.Equal(t, modeConfirm, m.mode)
	require.Equal(t, list.ItemID{ID: "window-2"}, m.confirm.Request.ItemID)
	m, cmd = updateModel(t, m, printableKey("y"))
	cmd()
	require.Equal(t, list.ItemID{ID: "window-2"}, requests[len(requests)-1].ItemID)
	require.True(t, requests[len(requests)-1].Confirmed)
}

func TestActionResultClearsPendingStateOnSuccessAndFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("refresh failed")} {
		m := browseModel(newModel(interactionSnapshot(), func(ActionRequest) (ActionResult, error) {
			return ActionResult{Message: "refreshed"}, failure
		}))
		m.err = errors.New("previous error")
		m, cmd := updateModel(t, m, controlKey('r'))
		require.True(t, m.busy)
		require.Equal(t, statusRefreshing, m.status)

		m, _ = updateModel(t, m, cmd())
		require.False(t, m.busy)
		require.Empty(t, m.status)
		require.Nil(t, m.pending)
		require.ErrorIs(t, m.err, failure)
	}
}

func TestInvalidRefreshRetainsSnapshotAndSelection(t *testing.T) {
	m := newModel(interactionSnapshot(), nil)
	m = selectNode(t, m, list.ItemID{ID: "window-2"})
	before := m.items.Snapshot()
	invalid := interactionSnapshot()
	invalid.Items[1].ID = list.ItemID{ID: "session-1"}

	m, _ = updateModel(t, m, actionResultMsg{result: ActionResult{Snapshot: &invalid}})
	require.Error(t, m.err)
	require.Equal(t, before, m.items.Snapshot())
	require.Equal(t, list.ItemID{ID: "window-2"}, selectedNodeID(m))
}

func TestPasteMsgFeedsFilterAndInputModes(t *testing.T) {
	m := newModel(interactionSnapshot(), nil)

	m.mode = modeFilter
	next, _ := m.Update(tea.PasteMsg{Content: "edi\n"})
	require.Equal(t, "edi", next.(model).items.Query())

	prompt := next.(model)
	prompt.mode = modeInput
	prompt.input = inputState{Value: "pre"}
	next, _ = prompt.Update(tea.PasteMsg{Content: "-pasted\nline\t\x1b[31mred\x1b[0m\x00\r\n"})
	require.Equal(t, "pre-pasted line red", next.(model).input.Value)
}
