package tui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/config"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestResolveThemeOverridesEveryRole(t *testing.T) {
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
ui: {view: tree, preview: true}
keymap: {normal: {toggle_preview: [p]}}
`
	preferences, err := config.Decode(strings.NewReader(input))
	require.NoError(t, err)
	rgb := func(v uint8) color.RGBA { return color.RGBA{R: v, G: v, B: v, A: 255} }

	got, warning := resolveConfig(preferences)

	require.Empty(t, warning)
	require.NoError(t, got.Validate())
	require.Equal(t, ui.Palette{
		Text:        rgb(1),
		Context:     rgb(23),
		Command:     rgb(24),
		Muted:       rgb(2),
		Hint:        rgb(3),
		Secondary:   rgb(4),
		Emphasis:    rgb(5),
		Active:      rgb(6),
		Accent:      rgb(7),
		Border:      rgb(8),
		Divider:     rgb(9),
		Working:     rgb(16),
		Blocked:     rgb(17),
		Idle:        rgb(18),
		Success:     rgb(19),
		Error:       rgb(20),
		SelectionFG: rgb(21),
		SelectionBG: rgb(22),
	}, got.Palette)
	require.Equal(t, ui.ViewTree, got.Initial.View)
	require.True(t, got.Initial.Preview)
	require.Equal(t, []string{"p"}, got.Keys.Keys(ui.KeyModeNormal, ui.ActionTogglePreview))
}

func TestResolveThemePartialOverridesAndFallbacks(t *testing.T) {
	for _, test := range []struct {
		name, theme string
		border      color.Color
		working     color.Color
		warning     string
	}{
		{"accent supplies border and working", "accent: '#123456'", lipgloss.Color("#123456"), lipgloss.Color("#123456"), ""},
		{"explicit dependent colors", "accent: '#123456', border: '#000000', working: '#ffffff'", lipgloss.Color("#000000"), lipgloss.Color("#ffffff"), ""},
		{"invalid border uses resolved accent", "accent: '#123456', border: blue", lipgloss.Color("#123456"), lipgloss.Color("#123456"), "theme.border"},
		{"invalid accent retains curated colors", "accent: blue", ui.DefaultPalette().Border, ui.DefaultPalette().Working, "theme.accent"},
		{"empty theme retains quiet border", "", ui.DefaultPalette().Border, ui.DefaultPalette().Working, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			preferences, err := config.Decode(strings.NewReader("ui: {view: tree}\ntheme: {error: '#ff0000', " + test.theme + "}"))
			require.NoError(t, err)

			got, warning := resolveConfig(preferences)

			require.NoError(t, got.Validate())
			require.Equal(t, test.border, got.Palette.Border)
			require.Equal(t, test.working, got.Palette.Working)
			require.Equal(t, lipgloss.NoColor{}, got.Palette.Text)
			require.Equal(t, lipgloss.Color("#ff0000"), got.Palette.Error)
			require.Equal(t, ui.ViewTree, got.Initial.View)
			if test.warning == "" {
				require.Empty(t, warning)
			} else {
				require.Contains(t, warning, test.warning)
				require.NotContains(t, warning, "blue")
			}
		})
	}
}

func TestThemeWarningsDoNotExposeValues(t *testing.T) {
	secret := "secret\x1b[31m\n" + strings.Repeat("x", 65536)
	preferences := config.Config{Theme: &config.Theme{Accent: &secret, Muted: &secret, SelectionBG: &secret}}

	got, warning := resolveConfig(preferences)

	require.Equal(t, ui.DefaultPalette(), got.Palette)
	require.NotContains(t, warning, "secret")
	require.NotContains(t, warning, "\x1b")
	require.Less(t, len(warning), 256)
	require.Equal(t, 3, strings.Count(warning, "theme."))
}

func TestPrepareServiceLoadsThemeWithNonfatalColorWarning(t *testing.T) {
	privateConfig(t, "ui: {view: tree}\nkeymap: {normal: {toggle_preview: [p]}}\ntheme: {accent: '#89B4FA', error: secret-value}")

	service, err := prepareService(t.Context(), nil, Overrides{})

	require.NoError(t, err)
	require.Equal(t, lipgloss.Color("#89b4fa"), service.Options.Palette.Accent)
	require.Equal(t, ui.DefaultPalette().Error, service.Options.Palette.Error)
	require.Equal(t, ui.ViewTree, service.Options.Initial.View)
	require.Equal(t, []string{"p"}, service.Options.Keys.Keys(ui.KeyModeNormal, ui.ActionTogglePreview))
	require.Contains(t, service.Notice, "theme.error")
	require.NotContains(t, service.Notice, "secret-value")
}

func TestThemeStructuralErrorUsesWholeConfigFallback(t *testing.T) {
	privateConfig(t, "ui: {view: tree}\ntheme: {accent: 110}")

	service, err := prepareService(t.Context(), nil, Overrides{})

	require.NoError(t, err)
	require.Equal(t, ui.DefaultOptions(), service.Options)
	require.Contains(t, service.Notice, "theme.accent")
}
