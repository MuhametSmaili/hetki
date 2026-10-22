package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/stretchr/testify/require"
)

func TestBoundModeTransitionsDiscardTheOwnedOverlay(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    uiMode
		keyMode KeyMode
	}{
		{name: "input to filter", mode: modeInput, keyMode: KeyModeInput},
		{name: "confirmation to filter", mode: modeConfirm, keyMode: KeyModeConfirm},
		{name: "menu to filter", mode: modeMenu, keyMode: KeyModeMenu},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys, err := ResolveKeyMap(map[KeyMode][]Binding{
				test.keyMode: {{Action: ActionFilter, Keys: []string{"z"}}},
			})
			require.NoError(t, err)
			m, err := newModelWithKeys(interactionSnapshot(), nil, keys)
			require.NoError(t, err)
			m = browseModel(m)
			m.mode = test.mode
			m.input = inputState{Value: "unfinished"}
			m.confirm = confirmState{Body: "unfinished"}
			m.menu = menuState{Title: "unfinished"}

			m, _ = updateModel(t, m, printableKey("z"))
			require.Equal(t, modeFilter, m.mode)
			switch test.mode {
			case modeInput:
				require.Equal(t, inputState{}, m.input)
			case modeConfirm:
				require.Equal(t, confirmState{}, m.confirm)
			case modeMenu:
				require.Equal(t, menuState{}, m.menu)
			}
		})
	}
}

func TestEmptyInputFeedbackIsVisibleAndClearsWhenEditingOrCanceling(t *testing.T) {
	newInputModel := func() model {
		m := browseModel(newModel(interactionSnapshot(), nil))
		m.mode = modeInput
		m.input = inputState{Title: "RENAME", Prompt: "Name", Value: " ", ReturnMode: modeBrowse}
		m.width, m.height = 80, 20
		return m.reflow()
	}

	m, _ := updateModel(t, newInputModel(), specialKey(tea.KeyEnter))
	require.ErrorContains(t, m.err, statusValueEmpty)
	require.Contains(t, terminal.Sanitize(m.View().Content), statusValueEmpty)
	for _, height := range []int{4, 6} {
		short := m
		short.width, short.height = 40, height
		short = short.reflow()
		lines := strings.Split(short.View().Content, "\n")
		visible := strings.Join(lines[:min(height, len(lines))], "\n")
		require.Contains(t, terminal.Sanitize(visible), statusValueEmpty, "height %d hides validation feedback", height)
	}

	m, _ = updateModel(t, m, specialKey(tea.KeyBackspace))
	require.NoError(t, m.err)

	m, _ = updateModel(t, newInputModel(), specialKey(tea.KeyEnter))
	m, _ = updateModel(t, m, printableKey("name"))
	require.NoError(t, m.err)

	m, _ = updateModel(t, newInputModel(), specialKey(tea.KeyEnter))
	m, _ = updateModel(t, m, tea.PasteMsg{Content: "name\n"})
	require.NoError(t, m.err)

	m, _ = updateModel(t, newInputModel(), specialKey(tea.KeyEnter))
	m, _ = updateModel(t, m, specialKey(tea.KeyEscape))
	require.NoError(t, m.err)
	require.NotContains(t, terminal.Sanitize(m.View().Content), statusValueEmpty)
}

func TestOverlayResultsRequireAPendingRequest(t *testing.T) {
	for _, result := range []ActionResult{
		{Menu: menuPtr(testItemMenu())},
		{Input: &InputPrompt{Title: "RENAME"}},
		{Confirmation: &Confirmation{Title: "DELETE"}},
	} {
		m := browseModel(newModel(interactionSnapshot(), nil))
		m.mode = modeFilter
		m.items.SetQuery("server")
		m, cmd := updateModel(t, m, actionResultMsg{result: result})
		require.Nil(t, cmd)
		require.ErrorContains(t, m.err, "no pending request")
		require.Equal(t, modeFilter, m.mode)
		require.Equal(t, "server", m.items.Query())
	}
}

func TestFilterAndInputShareEndOfLineEditingBehavior(t *testing.T) {
	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{name: "backspace", key: specialKey(tea.KeyBackspace), want: "one tw"},
		{name: "delete word", key: controlKey('w'), want: "one "},
		{name: "delete to start", key: controlKey('u'), want: ""},
		{name: "append", key: printableKey("界"), want: "one two界"},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter := browseModel(newModel(interactionSnapshot(), nil))
			filter.mode = modeFilter
			filter.items.SetQuery("one two")
			filter, _ = updateModel(t, filter, test.key)

			input := browseModel(newModel(interactionSnapshot(), nil))
			input.mode = modeInput
			input.input = inputState{Value: "one two"}
			input, _ = updateModel(t, input, test.key)

			require.Equal(t, test.want, filter.items.Query())
			require.Equal(t, test.want, input.input.Value)
		})
	}
}
