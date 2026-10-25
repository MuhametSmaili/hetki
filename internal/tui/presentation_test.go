package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/stretchr/testify/require"
)

func TestInlinePickerFitsItsHeightAndKeepsFiltering(t *testing.T) {
	options := DefaultOptions()
	options.Height = Height{Value: 12}
	options.Border = BorderNone
	m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
	require.NoError(t, err)
	require.Empty(t, m.View().Content, "wait for terminal dimensions before reserving rows")

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	view := m.View()

	require.False(t, view.AltScreen)
	require.Equal(t, 12, lipgloss.Height(view.Content))
	require.NotContains(t, terminal.Sanitize(view.Content), "╭")
	require.Contains(t, terminal.Sanitize(view.Content), "> █")

	m, _ = updateModel(t, m, printableKey("server"))
	require.Equal(t, "server", m.items.Query())
	require.Contains(t, terminal.Sanitize(m.View().Content), "> server█")
	m, _ = updateModel(t, m, controlKey('c'))
	require.Empty(t, m.View().Content, "remove only the inline region on exit")
	require.False(t, m.View().AltScreen)
}

func TestPresentationFitsSmallTerminalsAndOverlays(t *testing.T) {
	for _, border := range []BorderMode{BorderRounded, BorderNone} {
		for _, width := range []int{1, 3, 20, 80} {
			for _, height := range []int{1, 2, 3, 5, 10, 24} {
				t.Run(fmt.Sprintf("%s/%dx%d", border, width, height), func(t *testing.T) {
					options := DefaultOptions()
					options.Height, options.Border = Height{Value: 12}, border
					options.Initial.Preview = true
					m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
					require.NoError(t, err)
					m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: width, Height: height})
					for _, mode := range []uiMode{modeFilter, modeMenu, modeInput, modeConfirm} {
						m.mode = mode
						m.menu = menuState{Title: "ACTIONS", Entries: testItemMenu().Entries}
						m.input = inputState{Title: "RENAME", Prompt: "Name", Value: "hello"}
						m.confirm = confirmState{Title: "DELETE", Body: "Delete this session?"}

						view := m.View()

						assertInlineViewFits(t, view, width, min(12, max(1, height-1)))
					}
				})
			}
		}
	}
}

func assertInlineViewFits(t *testing.T, view tea.View, width, height int) {
	t.Helper()
	require.False(t, view.AltScreen)
	require.LessOrEqual(t, lipgloss.Height(view.Content), height)
	for _, line := range strings.Split(view.Content, "\n") {
		require.LessOrEqual(t, terminal.Width(line), width)
	}
}

func TestFullscreenPresentationRemainsDefaultAndBorderIsIndependent(t *testing.T) {
	for _, border := range []BorderMode{BorderRounded, BorderNone} {
		options := DefaultOptions()
		options.Border = border
		m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
		require.NoError(t, err)
		m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

		view := m.View()

		require.True(t, view.AltScreen)
		require.Equal(t, 24, lipgloss.Height(view.Content))
		require.Equal(t, border == BorderRounded, strings.Contains(terminal.Sanitize(view.Content), "╭"))
	}
}

func TestTinyInlinePickerKeepsSelectionVisible(t *testing.T) {
	for _, height := range []int{1, 2, 3} {
		for _, border := range []BorderMode{BorderNone, BorderRounded} {
			options := DefaultOptions()
			options.Height, options.Border = Height{Value: height}, border
			m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
			require.NoError(t, err)
			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

			selected, ok := m.selectedRow()
			require.True(t, ok)
			require.Contains(t, terminal.Sanitize(m.View().Content), selected.Item.Primary, "height %d", height)

			m, _ = updateModel(t, m, specialKey(tea.KeyDown))
			selected, ok = m.selectedRow()
			require.True(t, ok)
			require.Contains(t, terminal.Sanitize(m.View().Content), selected.Item.Primary, "height %d", height)

			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 2})
			selected, ok = m.selectedRow()
			require.True(t, ok)
			require.Contains(t, terminal.Sanitize(m.View().Content), selected.Item.Primary, "after shrink")
		}
	}
}

func TestInlineShortDialogsKeepActionableContent(t *testing.T) {
	for _, height := range []int{1, 2, 3, 4, 5} {
		options := DefaultOptions()
		options.Height = Height{Value: height}
		m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
		require.NoError(t, err)
		m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		m.mode = modeInput
		m.input = inputState{Title: "RENAME", Prompt: "Name", Value: "hello"}

		require.Contains(t, terminal.Sanitize(m.View().Content), "> hello_", "height %d", height)

		m.mode = modeConfirm
		m.confirm = confirmState{Title: "DELETE", Body: "Delete this session?"}

		require.Contains(t, terminal.Sanitize(m.View().Content), "Delete this session?", "height %d", height)
	}
}

func TestPresentationOptionsRejectInvalidValues(t *testing.T) {
	for _, change := range []func(*Options){
		func(o *Options) { o.Border = "" },
		func(o *Options) { o.Border = "other" },
		func(o *Options) { o.Height = Height{} },
		func(o *Options) { o.Height = Height{Value: -1} },
	} {
		options := DefaultOptions()
		change(&options)

		_, err := newModelWithOptions(t.Context(), viewSnapshot(), options, Runtime{})

		require.Error(t, err)
	}
}
