package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/directory"
	"github.com/stretchr/testify/require"
)

func TestDirectoryDeleteWordRemovesLastPathComponent(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "absolute path", value: "/work/api", want: "/work/"},
		{name: "trailing slash", value: "/work/api/", want: "/work/"},
		{name: "repeated trailing slashes", value: "/work/api//", want: "/work/"},
		{name: "relative path", value: "work/api", want: "work/"},
		{name: "home path", value: "~/api/", want: "~/"},
		{name: "spaces in component", value: "/work/my api", want: "/work/"},
		{name: "unicode", value: "/工作/项目", want: "/工作/"},
		{name: "single component", value: "api", want: ""},
		{name: "root", value: "/", want: ""},
		{name: "empty", value: "", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := formModel(t, nil)
			m.input.Fields[1].Value = test.value
			m, _ = updateModel(t, m, specialKey(tea.KeyTab))

			m, _ = updateModel(t, m, controlKey('w'))

			require.Equal(t, test.want, m.input.Fields[1].Value)
		})
	}
}

func TestDirectoryClearKeepsNameAndRelativeBase(t *testing.T) {
	m := formModel(t, nil)
	m.input.Fields[0].Value = "authored"
	m, _ = updateModel(t, m, specialKey(tea.KeyTab))
	m.directories.suggestions.Paths = []string{"/work/api/"}

	m, _ = updateModel(t, m, controlKey('l'))

	require.Empty(t, m.input.Fields[1].Value)
	require.Empty(t, m.directories.suggestions.Paths)
	require.Equal(t, "/work/api", m.input.Fields[1].BasePath)
	require.Equal(t, "authored", m.input.Fields[0].Value)

	m, _ = updateModel(t, m, printableKey("child"))
	value, err := m.input.Fields[1].submittedValue()

	require.NoError(t, err)
	require.Equal(t, "/work/api/child", value)
}

func TestTextFieldDeleteWordStillUsesWhitespace(t *testing.T) {
	m := formModel(t, nil)
	m.input.Fields[0].Value = "name with/slash"

	m, _ = updateModel(t, m, controlKey('w'))

	require.Equal(t, "name ", m.input.Fields[0].Value)
	require.Equal(t, "/work/api", m.input.Fields[1].Value)
}

func TestDirectorySuggestionNavigationKeys(t *testing.T) {
	for _, test := range []struct {
		name     string
		up, down tea.KeyPressMsg
	}{
		{name: "arrow keys", up: specialKey(tea.KeyUp), down: specialKey(tea.KeyDown)},
		{name: "control keys", up: controlKey('p'), down: controlKey('n')},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := formModel(t, nil)
			m, _ = updateModel(t, m, specialKey(tea.KeyTab))
			m.directories.suggestions.Paths = []string{"/work/api/", "/work/app/"}

			m, _ = updateModel(t, m, test.down)
			m, _ = updateModel(t, m, test.down)

			require.Equal(t, 1, m.directories.cursor, "selection stops at the last suggestion")

			m, _ = updateModel(t, m, test.up)
			m, _ = updateModel(t, m, test.up)

			require.Zero(t, m.directories.cursor, "selection stops at the first suggestion")

			m, _ = updateModel(t, m, test.down)
			m, _ = updateModel(t, m, specialKey(tea.KeyTab))

			require.Equal(t, "/work/app/", m.input.Fields[1].Value)
			require.Empty(t, m.input.Fields[0].Value)
		})
	}
}

func TestDirectoryNavigationWithoutSuggestionsKeepsValue(t *testing.T) {
	m := formModel(t, nil)
	m, _ = updateModel(t, m, specialKey(tea.KeyTab))

	m, _ = updateModel(t, m, controlKey('n'))
	m, _ = updateModel(t, m, controlKey('p'))

	require.Zero(t, m.directories.cursor)
	require.Equal(t, "/work/api", m.input.Fields[1].Value)
}

func TestConfiguredPromptBindingTakesPrecedenceOverDirectoryShortcuts(t *testing.T) {
	for _, key := range []rune{'n', 'l', 'w'} {
		t.Run("ctrl+"+string(key), func(t *testing.T) {
			m := formModel(t, nil)
			keys, err := ResolveKeyMap(map[KeyMode][]Binding{
				KeyModePrompt: {{Action: ActionCancel, Keys: []string{"ctrl+" + string(key)}}},
			})
			require.NoError(t, err)
			m.keys = keys
			m, _ = updateModel(t, m, specialKey(tea.KeyTab))
			m.directories.suggestions.Paths = []string{"/work/api/", "/work/app/"}

			m, _ = updateModel(t, m, controlKey(key))

			require.Equal(t, modeBrowse, m.mode)
			require.Empty(t, m.input.Fields)
		})
	}
}

func TestDirectorySuggestionsCoalesceEditsAndIgnoreStaleResults(t *testing.T) {
	m := formModel(t, nil)
	m.input.Focus = 1
	var calls []string
	m.readDirectories = func(ctx context.Context, value, _ string) (directory.Suggestions, error) {
		calls = append(calls, value)
		return directory.Suggestions{Paths: []string{value + "-match/"}}, ctx.Err()
	}
	m, first := m.syncDirectorySuggestions()
	require.NotNil(t, first)
	m.input.Fields[1].Value = "/work/new"
	m, next := m.syncDirectorySuggestions()
	require.Nil(t, next, "a second scan must wait for the canceled first scan")
	stale := first().(directoryResultMsg)
	require.ErrorIs(t, stale.err, context.Canceled)

	m, next = updateModel(t, m, stale)

	require.Empty(t, m.directories.suggestions.Paths)
	require.NotNil(t, next)
	m, _ = updateModel(t, m, next())
	require.Equal(t, []string{"/work/new-match/"}, m.directories.suggestions.Paths)
	require.Equal(t, []string{"/work/api", "/work/new"}, calls)
}

func TestCancelDiscardsDirectoryResultsEvenWhenSameFormReopens(t *testing.T) {
	m := formModel(t, nil)
	m.input.Focus = 1
	m.readDirectories = func(context.Context, string, string) (directory.Suggestions, error) {
		return directory.Suggestions{Paths: []string{"match/"}}, nil
	}
	m, cmd := m.syncDirectorySuggestions()
	m, _ = updateModel(t, m, specialKey(tea.KeyEscape))
	require.Equal(t, modeBrowse, m.mode)
	m.openOverlay(ActionResult{Input: new(exampleFormPrompt())}, &ActionRequest{ActionID: "example"})
	m.input.Focus = 1
	m, next := m.syncDirectorySuggestions()
	require.Nil(t, next)

	m, next = updateModel(t, m, cmd())

	require.Empty(t, m.directories.suggestions.Paths)
	require.NotNil(t, next)
	m, _ = updateModel(t, m, next())
	require.Equal(t, []string{"match/"}, m.directories.suggestions.Paths)
}

func TestSwitchingBetweenDirectoryFieldsInvalidatesSuggestions(t *testing.T) {
	m := formModel(t, nil)
	field := exampleFormPrompt().Fields[1]
	second := field
	second.ID = "other-folder"
	m.openOverlay(ActionResult{Input: &InputPrompt{Fields: []InputField{field, second}}}, &ActionRequest{ActionID: "example"})
	m.readDirectories = func(context.Context, string, string) (directory.Suggestions, error) {
		return directory.Suggestions{Paths: []string{"match/"}}, nil
	}
	m, cmd := m.syncDirectorySuggestions()
	m.input.focusField(1)
	m, next := m.syncDirectorySuggestions()
	require.Nil(t, next)

	m, next = updateModel(t, m, cmd())

	require.Empty(t, m.directories.suggestions.Paths, "same path in a different field must not accept the old scan")
	require.NotNil(t, next)
	m, _ = updateModel(t, m, next())
	require.Equal(t, []string{"match/"}, m.directories.suggestions.Paths)
}
