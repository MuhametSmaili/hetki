package tui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHexColor(t *testing.T) {
	for _, test := range []struct {
		name, value string
		want        color.RGBA
		valid       bool
	}{
		{"lowercase", "#89b4fa", color.RGBA{R: 137, G: 180, B: 250, A: 255}, true},
		{"uppercase", "#89B4FA", color.RGBA{R: 137, G: 180, B: 250, A: 255}, true},
		{"black", "#000000", color.RGBA{A: 255}, true},
		{"white", "#ffffff", color.RGBA{R: 255, G: 255, B: 255, A: 255}, true},
		{name: "empty"},
		{name: "name", value: "blue"},
		{name: "index", value: "110"},
		{name: "negative index", value: "-1"},
		{name: "default keyword", value: "default"},
		{name: "short hex", value: "#abc"},
		{name: "alpha", value: "#89b4fa80"},
		{name: "missing hash", value: "89b4fa"},
		{name: "invalid digit", value: "#89b4fg"},
		{name: "whitespace", value: " #89b4fa"},
		{name: "control", value: "#12\x1b[0m"},
		{name: "oversized", value: "#" + strings.Repeat("a", 65536)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseHexColor(test.value)

			if !test.valid {
				require.ErrorContains(t, err, "#RRGGBB")
				require.Zero(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestPaletteRequiresEveryResolvedColor(t *testing.T) {
	palette := DefaultPalette()
	for _, field := range []struct {
		name   string
		target *color.Color
	}{
		{"text", &palette.Text},
		{"context", &palette.Context},
		{"command", &palette.Command},
		{"muted", &palette.Muted},
		{"hint", &palette.Hint},
		{"secondary", &palette.Secondary},
		{"emphasis", &palette.Emphasis},
		{"active", &palette.Active},
		{"accent", &palette.Accent},
		{"border", &palette.Border},
		{"divider", &palette.Divider},
		{"working", &palette.Working},
		{"blocked", &palette.Blocked},
		{"idle", &palette.Idle},
		{"success", &palette.Success},
		{"error", &palette.Error},
		{"selection_fg", &palette.SelectionFG},
		{"selection_bg", &palette.SelectionBG},
	} {
		t.Run(field.name, func(t *testing.T) {
			original := *field.target
			*field.target = nil
			defer func() { *field.target = original }()

			err := palette.Validate()

			require.ErrorContains(t, err, "theme."+field.name)
		})
	}
}
