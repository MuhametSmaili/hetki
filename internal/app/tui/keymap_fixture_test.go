package tui

import (
	"bytes"
	"os"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/stretchr/testify/require"
)

func fullKeymapFixture(t testing.TB) string {
	t.Helper()
	contents, err := os.ReadFile("testdata/keymap.yaml")
	require.NoError(t, err)
	preferences, err := config.Decode(bytes.NewReader(contents))
	require.NoError(t, err)
	want := map[string][]string{
		"normal": {
			"toggle_preview", "quit", "move_up", "move_down", "move_top", "move_bottom",
			"page_up", "page_down", "filter", "next_match", "previous_match", "clear_filter",
			"context_menu", "create_session", "create_window", "rename_window", "rename_session",
			"delete_window", "delete_session", "expand", "collapse", "expand_all", "collapse_all",
			"refresh", "toggle_projection", "toggle_scope", "open", "last_session",
		},
		"filter": {
			"toggle_projection", "toggle_scope", "toggle_preview", "last_session", "cancel", "confirm",
			"move_up", "move_down", "page_up", "page_down", "backspace", "delete_word",
			"delete_to_start", "clear_filter", "context_menu",
		},
		"prompt":  {"cancel", "confirm", "backspace", "delete_word", "delete_to_start"},
		"confirm": {"cancel", "confirm"},
		"menu": {
			"open", "rename_window", "rename_session", "create_window", "create_session",
			"delete_window", "delete_session", "refresh", "toggle_projection",
			"cancel", "confirm", "move_up", "move_down",
		},
	}

	require.Len(t, preferences.Keymap, len(want), "fixture must configure every mode")
	for mode, actions := range want {
		require.Contains(t, preferences.Keymap, mode)
		require.Len(t, preferences.Keymap[mode], len(actions), "fixture mode %s must configure every action", mode)
		for _, action := range actions {
			require.Contains(t, preferences.Keymap[mode], action, "fixture mode %s", mode)
		}
	}
	return string(contents)
}
