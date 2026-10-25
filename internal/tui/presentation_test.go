package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
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

func TestHeaderlessInlinePickerShowsDiagnosticsBesideSelection(t *testing.T) {
	for _, tt := range []struct {
		name           string
		pickerRows     int
		terminalRows   int
		preview        bool
		failure        error
		wantDiagnostic string
	}{
		{name: "configured one row", pickerRows: 1, terminalRows: 24, wantDiagnostic: "config warning"},
		{name: "terminal shrinks to one row", pickerRows: 12, terminalRows: 2, failure: fmt.Errorf("refresh failed"), wantDiagnostic: "refresh failed"},
		{name: "one row with preview", pickerRows: 1, terminalRows: 24, preview: true, wantDiagnostic: "config warning"},
		{name: "error takes priority with preview", pickerRows: 1, terminalRows: 24, preview: true, failure: fmt.Errorf("refresh failed"), wantDiagnostic: "refresh failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			options := DefaultOptions()
			options.Height = Height{Value: tt.pickerRows}
			options.Initial.Preview = tt.preview
			snapshot := interactionSnapshot()
			snapshot.Notice = "config warning"
			m, err := newModelWithOptions(t.Context(), snapshot, options, Runtime{})
			require.NoError(t, err)
			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 200, Height: 24})
			if tt.failure != nil {
				m, _ = updateModel(t, m, actionResultMsg{err: tt.failure})
			}

			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 200, Height: tt.terminalRows})

			view := m.View()
			content := terminal.Sanitize(view.Content)
			require.Contains(t, content, tt.wantDiagnostic)
			require.Contains(t, content, "dev")
			if tt.failure != nil {
				require.NotContains(t, content, "config warning")
			}
			assertInlineViewFits(t, view, 200, 1)

			m, _ = updateModel(t, m, specialKey(tea.KeyDown))

			content = terminal.Sanitize(m.View().Content)
			require.Contains(t, content, "editor")
			require.Contains(t, content, tt.wantDiagnostic)
		})
	}
}

func TestHeaderlessInlineDiagnosticsStaySanitizedAndBounded(t *testing.T) {
	for _, tt := range []struct {
		width int
		want  string
	}{
		{width: 1, want: "f"},
		{width: 2, want: "fa"},
		{width: 3, want: "f"},
		{width: 20, want: "failur"},
		{width: 80, want: `failure界\nnext`},
	} {
		t.Run(fmt.Sprintf("width %d", tt.width), func(t *testing.T) {
			options := DefaultOptions()
			options.Height = Height{Value: 1}
			m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
			require.NoError(t, err)
			m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: tt.width, Height: 24})

			m, _ = updateModel(t, m, actionResultMsg{err: fmt.Errorf("failure界\x1b]0;secret\a\nnext")})

			view := m.View()
			require.Contains(t, terminal.Sanitize(view.Content), tt.want)
			require.NotContains(t, view.Content, "secret")
			assertInlineViewFits(t, view, tt.width, 1)
		})
	}
}

func TestHeaderlessInlinePickerShowsNoticeWithoutItems(t *testing.T) {
	options := DefaultOptions()
	options.Height = Height{Value: 1}
	snapshot := list.Snapshot{Notice: "config warning"}
	m, err := newModelWithOptions(t.Context(), snapshot, options, Runtime{})
	require.NoError(t, err)

	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	view := m.View()
	require.Contains(t, terminal.Sanitize(view.Content), "no sessions found")
	require.Contains(t, terminal.Sanitize(view.Content), "config warning")
	assertInlineViewFits(t, view, 80, 1)
}

func TestHeaderlessInlinePickerShowsStatusReadFailure(t *testing.T) {
	options := DefaultOptions()
	options.Height = Height{Value: 1}
	m, err := newModelWithOptions(t.Context(), interactionSnapshot(), options, Runtime{})
	require.NoError(t, err)
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	m, _ = updateModel(t, m, statusResultMsg{update: StatusUpdate{Err: fmt.Errorf("status unavailable")}})

	view := m.View()
	require.Contains(t, terminal.Sanitize(view.Content), "status unavailable")
	require.Contains(t, terminal.Sanitize(view.Content), "dev")
	assertInlineViewFits(t, view, 80, 1)
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
