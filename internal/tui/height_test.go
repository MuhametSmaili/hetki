package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

func TestParseHeight(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  Height
	}{
		{"12", Height{Value: 12}},
		{"100", Height{Value: 100}},
		{"40%", Height{Value: 40, Percent: true}},
		{"100%", Height{Value: 100, Percent: true}},
		{"1%", Height{Value: 1, Percent: true}},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseHeight(tt.input)

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
	for _, input := range []string{"", "0", "-1", "+12", "0%", "101%", "40%%", "%", "1.5%", "40 %", " 12", "12\n", "~40%", "0xc", "1_2", strings.Repeat("9", 1000)} {
		t.Run("invalid/"+strconv.Quote(input[:min(len(input), 30)]), func(t *testing.T) {
			_, err := ParseHeight(input)

			require.Error(t, err)
		})
	}
}

func TestHeightControlsBufferAndRecomputesRowsOnResize(t *testing.T) {
	for _, tt := range []struct {
		input      string
		rows40     int
		rows20     int
		fullscreen bool
	}{
		{"12", 12, 12, false},
		{"100", 39, 19, false},
		{"80%", 32, 16, false},
		{"99%", 39, 19, false},
		{"1%", 1, 1, false},
		{"100%", 40, 20, true},
	} {
		t.Run(tt.input, func(t *testing.T) {
			options := DefaultOptions()
			var err error
			options.Height, err = ParseHeight(tt.input)
			require.NoError(t, err)
			m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
			require.NoError(t, err)

			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 40})

			require.Equal(t, tt.fullscreen, m.View().AltScreen)
			require.Equal(t, tt.rows40, lipgloss.Height(m.View().Content))

			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})

			require.Equal(t, tt.fullscreen, m.View().AltScreen)
			require.Equal(t, tt.rows20, lipgloss.Height(m.View().Content))
		})
	}
}

func TestPreviewWidthAndPercentageHeightSurviveTogglesAndResize(t *testing.T) {
	options := DefaultOptions()
	options.Initial.Preview = true
	options.PreviewWidth = 45
	options.Height = Height{Value: 80, Percent: true}
	options.Border = BorderNone
	m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
	require.NoError(t, err)

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	require.Equal(t, 43, m.layout().previewWidth)
	require.Equal(t, 32, lipgloss.Height(m.View().Content))

	m, _ = updateModel(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt})

	require.False(t, m.preview.enabled)
	require.Zero(t, m.layout().previewWidth)
	require.Equal(t, 32, lipgloss.Height(m.View().Content))

	m, _ = updateModel(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt})
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})

	require.True(t, m.preview.enabled)
	require.Equal(t, 45, m.preview.width)
	require.Zero(t, m.layout().previewWidth)
	require.Equal(t, 16, lipgloss.Height(m.View().Content))

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	require.True(t, m.preview.enabled)
	require.Equal(t, 43, m.layout().previewWidth)
	require.Equal(t, 32, lipgloss.Height(m.View().Content))
}

func TestPercentageHeightArithmeticStaysBounded(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	m := model{height: maxInt, inline: true, pickerHeight: Height{Value: 99, Percent: true}}

	require.Positive(t, m.displayHeight())
	require.Less(t, m.displayHeight(), maxInt)
}
