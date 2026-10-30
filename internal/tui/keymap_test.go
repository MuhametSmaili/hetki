package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEveryDeclaredActionHasHandler(t *testing.T) {
	remaining := make(map[ActionID]struct{}, len(actionHandlers))
	for action, handler := range actionHandlers {
		require.NotNil(t, handler, "nil handler for %s", action)
		remaining[action] = struct{}{}
	}
	for _, action := range declaredActions {
		require.Contains(t, remaining, action, "missing handler for %s", action)
		delete(remaining, action)
	}
	require.Empty(t, remaining, "handlers exist for undeclared actions")
}

func TestResolveKeyMapRejectsSameModeCollisionsAndReservedQuit(t *testing.T) {
	_, err := ResolveKeyMap(map[KeyMode][]Binding{KeyModeNormal: {
		{Action: ActionQuit, Keys: []string{"q"}},
		{Action: ActionRefresh, Keys: []string{"q"}},
	}})
	require.ErrorContains(t, err, "is assigned")

	_, err = ResolveKeyMap(map[KeyMode][]Binding{KeyModeNormal: {
		{Action: ActionQuit, Keys: []string{"ctrl+c"}},
	}})
	require.ErrorContains(t, err, "reserved")
}

func TestModelUsesTheInjectedResolvedKeyMap(t *testing.T) {
	keys, err := ResolveKeyMap(map[KeyMode][]Binding{KeyModeNormal: {
		{Action: ActionRefresh, Keys: []string{"z"}},
	}})
	require.NoError(t, err)
	var got ActionRequest
	m, err := newModelWithKeys(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
		got = request
		return ActionResult{}, nil
	}, keys)
	require.NoError(t, err)
	m = browseModel(m)

	m, cmd := updateModel(t, m, printableKey("z"))
	require.True(t, m.busy)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, ActionRequest{ActionID: ActionRefresh}, got)
}

func TestResolveKeyMapRejectsRemovedJumpModeAndAction(t *testing.T) {
	for _, test := range []struct {
		name     string
		bindings map[KeyMode][]Binding
	}{
		{name: "mode", bindings: map[KeyMode][]Binding{"jump": {{Action: ActionMoveDown, Keys: []string{"ctrl+n"}}}}},
		{name: "empty mode", bindings: map[KeyMode][]Binding{"jump": nil}},
		{name: "action", bindings: map[KeyMode][]Binding{KeyModeNormal: {{Action: "jump", Keys: []string{";"}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveKeyMap(test.bindings)

			require.ErrorContains(t, err, `"jump"`)
		})
	}
}

func TestDefaultContextMenuBindingsAreScopedToNormalAndFilter(t *testing.T) {
	keys := DefaultKeyMap()
	require.Equal(t, []string{"ctrl+k"}, keys.Keys(KeyModeNormal, ActionContextMenu))
	require.Equal(t, []string{"ctrl+k"}, keys.Keys(KeyModeFilter, ActionContextMenu))
	require.Empty(t, keys.Keys(KeyModePrompt, ActionContextMenu))
	require.Empty(t, keys.Keys(KeyModeConfirm, ActionContextMenu))
	require.Empty(t, keys.Keys(KeyModeMenu, ActionContextMenu))
}

func TestDefaultMenuBindingsHaveTheirOwnControlsAndActions(t *testing.T) {
	keys := DefaultKeyMap()
	require.Equal(t, []string{"o"}, keys.Keys(KeyModeMenu, ActionOpen))
	require.Equal(t, []string{"enter", "ctrl+y"}, keys.Keys(KeyModeMenu, ActionConfirm))
	require.Equal(t, []string{"up", "k", "ctrl+p"}, keys.Keys(KeyModeMenu, ActionMoveUp))
	require.Equal(t, []string{"down", "j", "ctrl+n"}, keys.Keys(KeyModeMenu, ActionMoveDown))
	require.Equal(t, []string{"esc"}, keys.Keys(KeyModeMenu, ActionCancel))

	for _, test := range []struct {
		action ActionID
		keys   []string
	}{
		{ActionRename, []string{"r"}},
		{ActionRenameSession, []string{"R"}},
		{ActionCreateWindow, []string{"a"}},
		{ActionCreateSession, []string{"A"}},
		{ActionDelete, []string{"x"}},
		{ActionDeleteSession, []string{"X"}},
		{ActionRefresh, []string{"ctrl+r"}},
		{ActionToggleProjection, []string{"ctrl+t"}},
	} {
		t.Run(string(test.action), func(t *testing.T) {
			require.Equal(t, test.keys, keys.Keys(KeyModeMenu, test.action))
		})
	}
}

func TestDefaultConfirmationBindingsPreserveUppercaseChoices(t *testing.T) {
	keys := DefaultKeyMap()
	require.Contains(t, keys.Keys(KeyModeConfirm, ActionConfirm), "Y")
	require.Contains(t, keys.Keys(KeyModeConfirm, ActionCancel), "N")
}

func TestModalControlsUseTheResolvedBindings(t *testing.T) {
	keys, err := ResolveKeyMap(map[KeyMode][]Binding{KeyModePrompt: {
		{Action: ActionConfirm, Keys: []string{"z"}},
		{Action: ActionCancel, Keys: []string{"x"}},
	}})
	require.NoError(t, err)
	require.Equal(t, "z submit | x cancel", modalControls(keys, KeyModePrompt, "submit"))
}

func TestMenuControlsUseTheResolvedBindings(t *testing.T) {
	keys, err := ResolveKeyMap(map[KeyMode][]Binding{KeyModeMenu: {
		{Action: ActionMoveUp, Keys: []string{"w"}},
		{Action: ActionMoveDown, Keys: []string{"s"}},
		{Action: ActionConfirm, Keys: []string{"d"}},
		{Action: ActionCancel, Keys: []string{"a"}},
	}})
	require.NoError(t, err)
	require.Equal(t, "w/s move | d select | a cancel", menuControls(keys))
}

func TestResolveKeyMapAllowsTheSameKeyInDifferentModes(t *testing.T) {
	_, err := ResolveKeyMap(map[KeyMode][]Binding{
		KeyModeNormal: {{Action: ActionQuit, Keys: []string{"q"}}},
		KeyModeFilter: {{Action: ActionCancel, Keys: []string{"q"}}},
	})
	require.NoError(t, err)
}
