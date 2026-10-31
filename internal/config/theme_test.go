package config_test

import (
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDecodeThemePreservesAllOverrides(t *testing.T) {
	input := `theme:
  text: "#010101"
  context: "#171717"
  command: "#181818"
  muted: "#020202"
  hint: "#030303"
  secondary: "#040404"
  emphasis: "#050505"
  active: "#060606"
  accent: "#070707"
  border: "#080808"
  divider: "#090909"
  working: "#101010"
  blocked: "#111111"
  idle: "#121212"
  success: "#131313"
  error: "#141414"
  selection_fg: "#151515"
  selection_bg: "#161616"
ui: {view: tree}
keymap: {normal: {toggle_preview: [p]}}
`

	got, err := config.Decode(strings.NewReader(input))

	require.NoError(t, err)
	require.Equal(t, &config.Theme{
		Text:        ptr("#010101"),
		Context:     ptr("#171717"),
		Command:     ptr("#181818"),
		Muted:       ptr("#020202"),
		Hint:        ptr("#030303"),
		Secondary:   ptr("#040404"),
		Emphasis:    ptr("#050505"),
		Active:      ptr("#060606"),
		Accent:      ptr("#070707"),
		Border:      ptr("#080808"),
		Divider:     ptr("#090909"),
		Working:     ptr("#101010"),
		Blocked:     ptr("#111111"),
		Idle:        ptr("#121212"),
		Success:     ptr("#131313"),
		Error:       ptr("#141414"),
		SelectionFG: ptr("#151515"),
		SelectionBG: ptr("#161616"),
	}, got.Theme)
	require.Equal(t, ptr("tree"), got.UI.View)
	require.Equal(t, []string{"p"}, got.Keymap["normal"]["toggle_preview"])
}

func TestDecodeThemeDistinguishesOmittedAndInvalidStrings(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  *config.Theme
	}{
		{"omitted", "{}", nil},
		{"empty theme", "theme: {}", &config.Theme{}},
		{"empty override", "theme: {accent: ''}", &config.Theme{Accent: ptr("")}},
		{"invalid string remains a preference", "theme: {accent: blue}", &config.Theme{Accent: ptr("blue")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(test.input))

			require.NoError(t, err)
			require.Equal(t, test.want, got.Theme)
		})
	}
}

func TestDecodeThemeRejectsStructuralErrors(t *testing.T) {
	for _, test := range []struct{ name, input string }{
		{"null theme", "theme: null"},
		{"sequence", "theme: []"},
		{"tagged theme", "theme: !!map {}"},
		{"duplicate theme", "theme: {}\ntheme: {}"},
		{"unknown role", "theme: {secret-value: '#112233'}"},
		{"base selector", "theme: {base: terminal}"},
		{"duplicate role", "theme: {accent: '', accent: '#112233'}"},
		{"null override", "theme: {accent: null}"},
		{"unquoted hex is a comment", "theme:\n  accent: #112233"},
		{"integer", "theme: {accent: 110}"},
		{"boolean", "theme: {accent: false}"},
		{"nested mapping", "theme: {accent: {}}"},
		{"tagged string", "theme: {accent: !!str '#112233'}"},
		{"merge", "theme: {<<: {accent: '#112233'}}"},
		{"alias", "theme: {accent: &color '#112233', active: *color}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(test.input))

			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret-value")
			require.Equal(t, config.Config{}, got)
		})
	}
}
