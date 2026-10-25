package config_test

import (
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDecodePresentationPreferences(t *testing.T) {
	for _, tt := range []struct{ literal, want string }{
		{"12", "12"}, {"'12'", "12"}, {"40%", "40%"}, {"'100%'", "100%"},
		{"0xc", "12"}, {"0o14", "12"}, {"0b1100", "12"}, {"1_2", "12"},
		{"wrong", "wrong"}, {"'_12'", "_12"}, {"'08'", "08"},
		{"'1.5'", "1.5"}, {"'true'", "true"}, {"'0x10'", "0x10"},
	} {
		t.Run(tt.literal, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader("ui: {height: " + tt.literal + ", border: none}"))

			require.NoError(t, err)
			require.Equal(t, config.UI{Height: ptr(tt.want), Border: ptr("none")}, got.UI)
		})
	}
}

func TestDecodeHeightPreservesOutOfRangeChoicesForApplicationFallback(t *testing.T) {
	for _, value := range []string{"0", "-1", "101%", "9223372036854775808", "0x10000000000000000", "0" + strings.Repeat("7", 40), "1" + strings.Repeat("0", 309)} {
		got, err := config.Decode(strings.NewReader("ui: {view: tree, height: " + value + "}"))

		require.NoError(t, err)
		require.NotNil(t, got.UI.Height)
		require.Equal(t, ptr("tree"), got.UI.View)
	}
}

func TestDecodeRejectsInvalidPresentationStructure(t *testing.T) {
	for _, input := range []string{
		"ui: {height: 1.5}", "ui: {height: null}", "ui: {height: true}",
		"ui: {height: [12]}", "ui: {height: !!int 12}", "ui: {height: !!str '40%'}",
		"ui: {height: 12, height: 20}", "ui: {border: false}",
		"ui: {border: none, border: rounded}", "ui: {layout: inline}",
	} {
		t.Run(input, func(t *testing.T) {
			got, err := config.Decode(strings.NewReader(input))

			require.Error(t, err)
			require.Equal(t, config.Config{}, got)
		})
	}
}
