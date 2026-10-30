package tui

import (
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestInvalidKeymapFallsBackWithoutDiscardingUI(t *testing.T) {
	for _, test := range []struct{ name, keymap, problem string }{
		{"unknown mode", "{secret: {}}", "unsupported mode"},
		{"old input mode", "{input: {confirm: [ctrl+s]}}", "unsupported mode"},
		{"unknown action", "{filter: {secret: [z]}}", "unsupported action for this mode"},
		{"wrong mode for action", "{filter: {delete_window: [z]}}", "unsupported action for this mode"},
		{"ambiguous rename ID", "{normal: {rename: [v]}}", "unsupported action for this mode"},
		{"ambiguous delete ID", "{normal: {delete: [v]}}", "unsupported action for this mode"},
		{"unknown key", "{filter: {toggle_preview: [secret]}}", "invalid key syntax"},
		{"control sequence in key", "{filter: {toggle_preview: [\"secret\\e[31m\"]}}", "invalid key syntax"},
		{"reserved quit", "{filter: {toggle_preview: [ctrl+c]}}", "ctrl+c is reserved for emergency quit"},
		{"default collision", "{filter: {toggle_preview: [tab]}}", "conflicting or duplicate bindings"},
		{"menu collision", "{menu: {rename_window: [o]}}", "conflicting or duplicate bindings"},
		{"duplicate key", "{filter: {toggle_preview: [alt+v, alt+v]}}", "conflicting or duplicate bindings"},
		{"empty cancel", "{prompt: {cancel: []}}", "each action requires 1–16 keys"},
		{"empty confirm", "{confirm: {confirm: []}}", "each action requires 1–16 keys"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := "ui: {view: tree, scope: tracked}\nkeymap: " + test.keymap
			preferences, err := config.Decode(strings.NewReader(input))
			require.NoError(t, err)

			options, warning := resolveConfig(preferences)

			require.Equal(t, ui.DefaultKeyMap(), options.Keys)
			require.Equal(t, ui.ViewTree, options.Initial.View)
			require.Equal(t, ui.ScopeTracked, options.Initial.Scope)
			require.Equal(t, "keymap: "+test.problem+"; using default keymap", warning)
			require.NotContains(t, warning, "secret")
			require.NotContains(t, warning, "\x1b")
		})
	}
}

func TestKeymapFailureDoesNotApplyPartialOverrides(t *testing.T) {
	privateConfig(t, `
ui: {view: tree}
keymap:
  normal:
    toggle_preview: [p]
  prompt:
    confirm: [ctrl+c]
`)

	service, err := prepareService(t.Context(), nil, Overrides{})

	require.NoError(t, err)
	require.Equal(t, ui.DefaultKeyMap(), service.Options.Keys)
	require.Equal(t, ui.ViewTree, service.Options.Initial.View)
	require.Contains(t, service.Notice, "keymap")
	require.NotContains(t, service.Notice, "configuration ignored")
}

func TestEmptyKeymapPreservesDefaults(t *testing.T) {
	for _, input := range []string{"", "keymap: {}", "keymap: {normal: {}, menu: {}}"} {
		t.Run(input, func(t *testing.T) {
			preferences, err := config.Decode(strings.NewReader(input))
			require.NoError(t, err)

			options, warning := resolveConfig(preferences)

			require.Empty(t, warning)
			require.Equal(t, ui.DefaultOptions(), options)
		})
	}
}

func TestFullKeymapFixturePreservesDefaults(t *testing.T) {
	contents := fullKeymapFixture(t)
	preferences, err := config.Decode(strings.NewReader(contents))
	require.NoError(t, err)

	options, warning := resolveConfig(preferences)

	require.Empty(t, warning)
	require.Equal(t, ui.DefaultKeyMap(), options.Keys)
}
