package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/config"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestResolveConfigIndependentMenuActions(t *testing.T) {
	preferences, err := config.Decode(strings.NewReader(
		"keymap: {normal: {rename_window: [o]}, menu: {rename_window: [w], delete_window: [d], create_window: [n]}}"))
	require.NoError(t, err)

	options, warning := resolveConfig(preferences)

	require.Empty(t, warning)
	require.Equal(t, []string{"o"}, options.Keys.Keys(ui.KeyModeNormal, ui.ActionRename))
	require.Equal(t, []string{"w"}, options.Keys.Keys(ui.KeyModeMenu, ui.ActionRename))
	require.Equal(t, []string{"x"}, options.Keys.Keys(ui.KeyModeNormal, ui.ActionDelete))
	require.Equal(t, []string{"d"}, options.Keys.Keys(ui.KeyModeMenu, ui.ActionDelete))
	require.Equal(t, []string{"a"}, options.Keys.Keys(ui.KeyModeNormal, ui.ActionCreateWindow))
	require.Equal(t, []string{"n"}, options.Keys.Keys(ui.KeyModeMenu, ui.ActionCreateWindow))
	require.Equal(t, []string{"o"}, options.Keys.Keys(ui.KeyModeMenu, ui.ActionOpen))
}

func TestResolveConfigKeymapOverrides(t *testing.T) {
	preferences, err := config.Decode(strings.NewReader(`
ui: {view: tree}
keymap:
  normal:
    toggle_preview: ["p"]
  filter:
    toggle_preview: ["alt+v"]
  prompt:
    confirm: ["ctrl+s"]
  confirm:
    cancel: ["c"]
  menu:
    move_down: ["s"]
`))
	require.NoError(t, err)

	options, warning := resolveConfig(preferences)

	require.Empty(t, warning)
	require.Equal(t, ui.ViewTree, options.Initial.View)
	require.NoError(t, options.Validate())
	for _, test := range []struct {
		mode   ui.KeyMode
		action ui.ActionID
		keys   []string
	}{
		{ui.KeyModeNormal, ui.ActionTogglePreview, []string{"p"}},
		{ui.KeyModeFilter, ui.ActionTogglePreview, []string{"alt+v"}},
		{ui.KeyModePrompt, ui.ActionConfirm, []string{"ctrl+s"}},
		{ui.KeyModeConfirm, ui.ActionCancel, []string{"c"}},
		{ui.KeyModeMenu, ui.ActionMoveDown, []string{"s"}},
		{ui.KeyModeNormal, ui.ActionOpen, []string{"enter", "ctrl+y"}},
		{ui.KeyModePrompt, ui.ActionCancel, []string{"esc"}},
	} {
		t.Run(string(test.mode)+"/"+string(test.action), func(t *testing.T) {
			require.Equal(t, test.keys, options.Keys.Keys(test.mode, test.action))
		})
	}
	action, matched := options.Keys.Action(ui.KeyModeNormal, tea.KeyPressMsg{Code: 'p', Text: "p"})
	require.True(t, matched)
	require.Equal(t, ui.ActionTogglePreview, action)
	require.False(t, options.Keys.Matches(ui.KeyModeNormal, ui.ActionTogglePreview, tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt}))
}
