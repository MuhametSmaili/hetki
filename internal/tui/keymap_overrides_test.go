package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestKeymapOverridesReplaceBindingsTogetherWithoutChangingDefaults(t *testing.T) {
	defaults := DefaultKeyMap()
	overrides := map[KeyMode][]Binding{KeyModeNormal: {
		{Action: ActionMoveUp, Keys: []string{"j"}},
		{Action: ActionMoveDown, Keys: []string{"k"}},
	}}

	keys, err := defaults.WithOverrides(overrides)

	require.NoError(t, err)
	require.Equal(t, []string{"j"}, keys.Keys(KeyModeNormal, ActionMoveUp))
	require.Equal(t, []string{"k"}, keys.Keys(KeyModeNormal, ActionMoveDown))
	require.Equal(t, []string{"up", "k", "ctrl+p"}, defaults.Keys(KeyModeNormal, ActionMoveUp))
	require.Equal(t, []string{"down", "j", "ctrl+n"}, defaults.Keys(KeyModeNormal, ActionMoveDown))
	require.Equal(t, []string{"down", "j", "ctrl+n"}, keys.Keys(KeyModeMenu, ActionMoveDown))
	overrides[KeyModeNormal][0].Keys[0] = "changed"
	require.Equal(t, []string{"j"}, keys.Keys(KeyModeNormal, ActionMoveUp))
}

func TestKeymapOverridesRejectInvalidBindings(t *testing.T) {
	for _, test := range []struct {
		name      string
		overrides map[KeyMode][]Binding
		cause     error
	}{
		{"unknown mode", map[KeyMode][]Binding{"other": {}}, ErrUnknownKeyMode},
		{"unknown action", map[KeyMode][]Binding{KeyModeNormal: {{Action: "other", Keys: []string{"z"}}}}, ErrUnsupportedKeyAction},
		{"unsupported action in mode", map[KeyMode][]Binding{KeyModePrompt: {{Action: ActionDelete, Keys: []string{"z"}}}}, ErrUnsupportedKeyAction},
		{"duplicate action", map[KeyMode][]Binding{KeyModeNormal: {{Action: ActionQuit, Keys: []string{"z"}}, {Action: ActionQuit, Keys: []string{"v"}}}}, ErrKeyBindingConflict},
		{"duplicate key", map[KeyMode][]Binding{KeyModeNormal: {{Action: ActionQuit, Keys: []string{"z", "z"}}}}, ErrKeyBindingConflict},
		{"default collision", map[KeyMode][]Binding{KeyModeNormal: {{Action: ActionQuit, Keys: []string{"j"}}}}, ErrKeyBindingConflict},
		{"override collision", map[KeyMode][]Binding{KeyModeNormal: {{Action: ActionQuit, Keys: []string{"z"}}, {Action: ActionRefresh, Keys: []string{"z"}}}}, ErrKeyBindingConflict},
		{"reserved quit", map[KeyMode][]Binding{KeyModeConfirm: {{Action: ActionConfirm, Keys: []string{"ctrl+c"}}}}, ErrReservedQuitKey},
		{"missing cancel", map[KeyMode][]Binding{KeyModePrompt: {{Action: ActionCancel}}}, ErrInvalidKeyCount},
		{"missing confirm", map[KeyMode][]Binding{KeyModeConfirm: {{Action: ActionConfirm, Keys: []string{}}}}, ErrInvalidKeyCount},
		{"too many keys", map[KeyMode][]Binding{KeyModePrompt: {{Action: ActionConfirm, Keys: strings.Split("abcdefghijklmnopq", "")}}}, ErrInvalidKeyCount},
		{"menu item conflicts with open", map[KeyMode][]Binding{KeyModeMenu: {{Action: ActionRename, Keys: []string{"o"}}}}, ErrKeyBindingConflict},
		{"menu control conflicts with menu item", map[KeyMode][]Binding{KeyModeMenu: {{Action: ActionCancel, Keys: []string{"r"}}}}, ErrKeyBindingConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			defaults := DefaultKeyMap()

			_, err := defaults.WithOverrides(test.overrides)

			require.ErrorIs(t, err, test.cause)
			require.Equal(t, DefaultKeyMap(), defaults)
		})
	}
}

func TestKeymapOverrideErrorsRetainContext(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    KeyMode
		keys    []string
		cause   error
		context string
	}{
		{
			name: "reserved key", mode: KeyModeConfirm, keys: []string{"ctrl+c"}, cause: ErrReservedQuitKey,
			context: `mode "confirm" action "confirm" key "ctrl+c"`,
		},
		{
			name: "invalid second key", mode: KeyModePrompt, keys: []string{"ctrl+s", "secret"}, cause: ErrInvalidKeySyntax,
			context: `mode "prompt" action "confirm": key 2 "secret"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
				test.mode: {{Action: ActionConfirm, Keys: test.keys}},
			})

			require.ErrorIs(t, err, test.cause)
			require.ErrorContains(t, err, test.context)
		})
	}
}

func TestKeymapOverridesAcceptMaximumKeys(t *testing.T) {
	bindings := strings.Split("abcdefghijklmnop", "")

	keys, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
		KeyModePrompt: {{Action: ActionConfirm, Keys: bindings}},
	})

	require.NoError(t, err)
	require.Equal(t, bindings, keys.Keys(KeyModePrompt, ActionConfirm))
}

func TestKeymapOverridesMatchCanonicalKeyEvents(t *testing.T) {
	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"G", tea.KeyPressMsg{Code: 'g', Text: "G", Mod: tea.ModShift}},
		{"?", tea.KeyPressMsg{Code: '/', Text: "?", Mod: tea.ModShift}},
		{"+", tea.KeyPressMsg{Code: '=', Text: "+", Mod: tea.ModShift}},
		{"é", tea.KeyPressMsg{Code: 'é', Text: "é"}},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
		{"f12", tea.KeyPressMsg{Code: tea.KeyF12}},
		{"shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}},
		{"ctrl+alt+p", tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModAlt}},
		{"ctrl+shift+p", tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModShift}},
		{"alt++", tea.KeyPressMsg{Code: '+', Mod: tea.ModAlt}},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
				KeyModePrompt: {{Action: ActionConfirm, Keys: []string{test.name}}},
			})
			require.NoError(t, err)

			require.True(t, keys.Matches(KeyModePrompt, ActionConfirm, test.key))
			require.Equal(t, test.name+" submit | esc cancel", modalControls(keys, KeyModePrompt, "submit"))
		})
	}
}

func TestKeymapOverridesRejectInvalidKeySyntax(t *testing.T) {
	for _, value := range []string{
		"", " ", " ctrl+s", "ctrl+s ", "escape", "pageup", "Ctrl+s", "ctrl+S",
		"shift+s", "ctrl+ctrl+s", "alt+ctrl+s", "ctrl+", "shift+", "s s", "gg",
		"f0", "f25", "f01", "control+s", "\x1b", "\n", "ctrl+\n", "\xff",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
				KeyModePrompt: {{Action: ActionConfirm, Keys: []string{value}}},
			})

			require.ErrorIs(t, err, ErrInvalidKeySyntax)
		})
	}
}

func TestConfiguredMenuUsesIndependentShortcuts(t *testing.T) {
	keys, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
		KeyModeNormal: {{Action: ActionRename, Keys: []string{"v"}}},
		KeyModeMenu: {
			{Action: ActionRename, Keys: []string{"z"}},
			{Action: ActionMoveUp, Keys: []string{"w"}},
			{Action: ActionMoveDown, Keys: []string{"s"}},
			{Action: ActionConfirm, Keys: []string{"ctrl+s"}},
			{Action: ActionCancel, Keys: []string{"c"}},
		},
	})
	require.NoError(t, err)
	var got ActionRequest
	m, err := newModelWithKeys(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
		got = request
		return ActionResult{Menu: menuPtr(testItemMenu())}, nil
	}, keys)
	require.NoError(t, err)
	m = openTestMenu(t, browseModel(m))

	m, _ = updateModel(t, m, printableKey("s"))

	require.Equal(t, 1, m.menu.Cursor)
	require.Equal(t, "w/s move | ctrl+s select | c cancel", menuControls(keys))
	require.Contains(t, m.viewOverlay(100, 20), "‹z›")
	m, cmd := updateModel(t, m, printableKey("v"))
	require.Nil(t, cmd, "normal shortcuts do not leak into the menu")
	require.Equal(t, modeMenu, m.mode)
	m, cmd = updateModel(t, m, printableKey("z"))
	require.True(t, m.busy)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, ActionRename, got.ActionID)
}

func TestSameKeyDispatchesDifferentActionsInNormalAndMenu(t *testing.T) {
	keys, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
		KeyModeMenu: {
			{Action: ActionRename, Keys: []string{"w"}},
			{Action: ActionDelete, Keys: []string{"r"}},
		},
	})
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		inMenu bool
		key    string
		action ActionID
	}{
		{"normal rename", false, "r", ActionRename},
		{"menu delete", true, "r", ActionDelete},
		{"menu rename", true, "w", ActionRename},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got ActionRequest
			m, err := newModelWithKeys(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
				got = request
				return ActionResult{Menu: menuPtr(testItemMenu())}, nil
			}, keys)
			require.NoError(t, err)
			m = browseModel(m)
			if test.inMenu {
				m = openTestMenu(t, m)
			}

			_, cmd := updateModel(t, m, printableKey(test.key))

			require.NotNil(t, cmd)
			cmd()
			require.Equal(t, test.action, got.ActionID)
		})
	}
}

func TestConfiguredKeysDriveModelAndHints(t *testing.T) {
	keys, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
		KeyModeNormal: {{Action: ActionTogglePreview, Keys: []string{"v"}}, {Action: ActionFilter, Keys: []string{"f"}}},
		KeyModeFilter: {{Action: ActionTogglePreview, Keys: []string{"alt+v"}}},
	})
	require.NoError(t, err)
	m, err := newModelWithKeys(interactionSnapshot(), nil, keys)
	require.NoError(t, err)
	m = browseModel(m)

	m, _ = updateModel(t, m, printableKey("v"))

	require.True(t, m.preview.enabled)
	require.Equal(t, "press f to filter", headerHint(m))
	m, _ = updateModel(t, m, printableKey("f"))
	require.Equal(t, modeFilter, m.mode)
	m, _ = updateModel(t, m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModAlt})
	require.False(t, m.preview.enabled)
	m, cmd := updateModel(t, m, controlKey('c'))
	require.True(t, m.quitting)
	require.NotNil(t, cmd)
}
