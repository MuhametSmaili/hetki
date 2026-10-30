package config_test

import (
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDecodeKeymap(t *testing.T) {
	input := `
ui: {view: tree}
keymap:
  normal:
    move_down: ["j", "down", "ctrl+n"]
    toggle_preview: ["p"]
  filter:
    toggle_preview: ["alt+v"]
  menu: {}
`

	got, err := config.Decode(strings.NewReader(input))

	require.NoError(t, err)
	require.Equal(t, config.Config{
		UI: config.UI{View: ptr("tree")},
		Keymap: map[string]map[string][]string{
			"normal": {"move_down": {"j", "down", "ctrl+n"}, "toggle_preview": {"p"}},
			"filter": {"toggle_preview": {"alt+v"}},
			"menu":   {},
		},
	}, got)
}

func TestDecodeRejectsMalformedKeymap(t *testing.T) {
	for _, test := range []struct{ name, input string }{
		{"null", "keymap: null"},
		{"list", "keymap: []"},
		{"tagged map", "keymap: !!map {}"},
		{"duplicate root", "keymap: {}\nkeymap: {}"},
		{"null mode", "keymap: {normal: null}"},
		{"list mode", "keymap: {normal: []}"},
		{"duplicate mode", "keymap: {normal: {}, normal: {}}"},
		{"nonstring mode", "keymap: {true: {}}"},
		{"tagged mode", "keymap: {normal: !!map {}}"},
		{"duplicate action", "keymap: {normal: {quit: [], quit: []}}"},
		{"nonstring action", "keymap: {normal: {true: []}}"},
		{"scalar keys", "keymap: {normal: {quit: q}}"},
		{"null keys", "keymap: {normal: {quit: null}}"},
		{"mapping keys", "keymap: {normal: {quit: {}}}"},
		{"tagged keys", "keymap: {normal: {quit: !!seq [q]}}"},
		{"nonstring key", "keymap: {normal: {quit: [1]}}"},
		{"null key", "keymap: {normal: {quit: [null]}}"},
		{"nested key", "keymap: {normal: {quit: [[q]]}}"},
		{"tagged key", "keymap: {normal: {quit: [!!str q]}}"},
		{"alias", "keymap: {normal: {quit: [&q q, *q]}}"},
		{"merge", "keymap: {normal: {<<: {quit: [q]}}}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := "ui: {view: tree}\n" + test.input

			got, err := config.Decode(strings.NewReader(input))

			require.Error(t, err)
			require.Equal(t, config.Config{}, got)
		})
	}
}

func TestDecodeKeymapDoesNotEchoSuppliedValues(t *testing.T) {
	for _, input := range []string{
		"keymap: {secret: {}, secret: {}}",
		"keymap: {normal: {secret: [], secret: []}}",
		"keymap: {normal: {quit: !secret []}}",
	} {
		_, err := config.Decode(strings.NewReader(input))

		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}
