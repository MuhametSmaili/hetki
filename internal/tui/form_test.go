package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/directory"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/stretchr/testify/require"
)

func exampleFormPrompt() InputPrompt {
	return InputPrompt{
		Title: "EXAMPLE", SubmitStatus: "saving...", SubmitLabel: "save",
		Fields: []InputField{
			{ID: "title", Label: "Title", Kind: InputText, Placeholder: "Type a title…", Required: true},
			{ID: "folder", Label: "Folder", Kind: InputDirectory, InitialValue: "/work/api", BasePath: "/work/api", Required: true},
		},
	}
}

func formModel(t *testing.T, dispatch DispatchFunc) model {
	t.Helper()
	m := browseModel(newModel(interactionSnapshot(), dispatch))
	m.ctx = t.Context()
	m.width, m.height = 100, 24
	m.openOverlay(ActionResult{Input: new(exampleFormPrompt())}, &ActionRequest{ActionID: "example"})
	require.NoError(t, m.err)
	return m
}

func TestFormStartsWithEmptyFocusedTextAndVisiblePlaceholder(t *testing.T) {
	m := formModel(t, nil)

	view := terminal.Sanitize(m.View().Content)

	require.Zero(t, m.input.Focus)
	require.Empty(t, m.input.Fields[0].Value)
	require.Contains(t, view, "_ Type a title…")
	require.Contains(t, view, "/work/api")

	m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))

	require.Nil(t, cmd)
	require.ErrorContains(t, m.err, "Title is required")
	require.Empty(t, m.input.Fields[0].Value, "placeholder text is never submitted as a value")
}

func TestFormEditsFieldsAndCompletesChosenDirectory(t *testing.T) {
	m := formModel(t, nil)
	m, _ = updateModel(t, m, printableKey("authored"))
	m, _ = updateModel(t, m, specialKey(tea.KeyTab))
	m, _ = updateModel(t, m, controlKey('u'))
	m, _ = updateModel(t, m, tea.PasteMsg{Content: "~/Projects/ap\n"})
	m.directories.suggestions.Paths = []string{"~/Projects/api/", "~/Projects/app/"}

	m, _ = updateModel(t, m, specialKey(tea.KeyDown))
	m, _ = updateModel(t, m, specialKey(tea.KeyTab))
	m, _ = updateModel(t, m, specialKey(tea.KeyBackspace))
	m, _ = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})

	require.Equal(t, "authored", m.input.Fields[0].Value)
	require.Equal(t, "~/Projects/app", m.input.Fields[1].Value)
	require.Zero(t, m.input.Focus)
}

func TestFocusedDirectoryEditsExistingValue(t *testing.T) {
	for _, test := range []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{name: "typing appends", msg: printableKey("/child"), want: "/work/api/child"},
		{name: "paste appends", msg: tea.PasteMsg{Content: "/child\n"}, want: "/work/api/child"},
		{name: "explicit clear", msg: controlKey('u'), want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := formModel(t, nil)
			m, _ = updateModel(t, m, specialKey(tea.KeyTab))

			m, _ = updateModel(t, m, test.msg)

			require.Equal(t, test.want, m.input.Fields[1].Value)
		})
	}
}

func TestDirectoryBackspaceRemovesOneCharacterAfterFocus(t *testing.T) {
	for _, focus := range []string{"initial field", "tab to directory", "return to directory"} {
		t.Run(focus, func(t *testing.T) {
			m := formModel(t, nil)
			if focus == "initial field" {
				prompt := exampleFormPrompt()
				prompt.Fields = prompt.Fields[1:]
				m.openOverlay(ActionResult{Input: &prompt}, &ActionRequest{ActionID: "example"})
			} else {
				m, _ = updateModel(t, m, specialKey(tea.KeyTab))
			}
			if focus == "return to directory" {
				m, _ = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
				m, _ = updateModel(t, m, specialKey(tea.KeyTab))
			}

			m, _ = updateModel(t, m, specialKey(tea.KeyBackspace))

			require.Equal(t, "/work/ap", m.input.activeField().Value)
		})
	}
}

func TestFormSubmitsValuesByIDAndResolvesDirectory(t *testing.T) {
	var got ActionRequest
	m := formModel(t, func(request ActionRequest) (ActionResult, error) {
		got = request
		return ActionResult{}, nil
	})
	m.input.Fields[0].Value = "authored"
	m.input.Fields[1].Value = "../web.v2"

	m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())

	require.Equal(t, map[string]string{"title": "authored", "folder": "/work/web.v2"}, got.Values)
	require.Nil(t, got.Value)
	require.Equal(t, modeBrowse, m.mode)
	require.False(t, m.busy)
	require.Equal(t, inputState{}, m.input)
}

func TestFormFailureRetainsValuesForCorrection(t *testing.T) {
	failure := errors.New("save failed")
	m := formModel(t, func(ActionRequest) (ActionResult, error) { return ActionResult{}, failure })
	m.input.Fields[0].Value, m.input.Fields[1].Value = "authored", "/work/wrong"

	m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))
	m, _ = updateModel(t, m, cmd())

	require.ErrorIs(t, m.err, failure)
	require.Equal(t, modeInput, m.mode)
	require.Equal(t, "authored", m.input.Fields[0].Value)
	require.Equal(t, "/work/wrong", m.input.Fields[1].Value)
	require.Contains(t, terminal.Sanitize(m.View().Content), failure.Error())

	m, _ = updateModel(t, m, specialKey(tea.KeyTab))
	m, _ = updateModel(t, m, controlKey('l'))
	m, _ = updateModel(t, m, printableKey("/work/correct"))

	require.NoError(t, m.err)
	require.Equal(t, "/work/correct", m.input.Fields[1].Value)
}

func TestRequiredFieldsFocusInvalidInputWithoutDispatch(t *testing.T) {
	for _, test := range []struct {
		name, title, folder, errorText string
		focus                          int
	}{
		{name: "empty text", folder: "/work/api", errorText: "Title is required"},
		{name: "whitespace text", title: "  ", folder: "/work/api", errorText: "Title is required"},
		{name: "empty directory", title: "authored", errorText: "Folder is required", focus: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := formModel(t, nil)
			m.input.Fields[0].Value, m.input.Fields[1].Value = test.title, test.folder

			m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))

			require.Nil(t, cmd)
			require.ErrorContains(t, m.err, test.errorText)
			require.Equal(t, test.focus, m.input.Focus)
			require.Equal(t, modeInput, m.mode)
		})
	}
}

func TestDirectoryFieldWorksAloneAndBeforeTextFields(t *testing.T) {
	for _, textFields := range []int{0, 2} {
		t.Run(fmt.Sprintf("%d_text_fields", textFields), func(t *testing.T) {
			m := formModel(t, nil)
			fields := []InputField{{ID: "location", Label: "Location", Kind: InputDirectory, InitialValue: "/work", BasePath: "/work"}}
			for i := range textFields {
				fields = append(fields, InputField{ID: fmt.Sprintf("text-%d", i), Label: "Text", Kind: InputText})
			}
			m.openOverlay(ActionResult{Input: &InputPrompt{Fields: fields}}, &ActionRequest{ActionID: "example"})

			m, _ = updateModel(t, m, printableKey("/child"))
			m, _ = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})

			require.Equal(t, "/work/child", m.input.Fields[0].Value)
			require.Equal(t, len(fields)-1, m.input.Focus)
		})
	}
}

func TestFormRejectsInvalidFieldDefinitions(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields []InputField
	}{
		{name: "missing ID", fields: []InputField{{Label: "Text", Kind: InputText}}},
		{name: "missing label", fields: []InputField{{ID: "text", Kind: InputText}}},
		{name: "duplicate ID", fields: []InputField{{ID: "same", Label: "First", Kind: InputText}, {ID: "same", Label: "Second", Kind: InputText}}},
		{name: "unknown input kind", fields: []InputField{{ID: "text", Label: "Text", Kind: "unknown"}}},
		{name: "relative directory base", fields: []InputField{{ID: "path", Label: "Path", Kind: InputDirectory, BasePath: "relative"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := browseModel(newModel(interactionSnapshot(), nil))

			m.openOverlay(ActionResult{Input: &InputPrompt{Fields: test.fields}}, &ActionRequest{ActionID: "example"})

			require.Error(t, m.err)
			require.Equal(t, modeBrowse, m.mode)
		})
	}
}

func TestFormRenderingFitsShortAndNarrowPickers(t *testing.T) {
	for _, size := range []struct{ width, height int }{{100, 24}, {40, 10}, {20, 6}, {12, 3}, {8, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := formModel(t, nil)
			m.input.Focus = 1
			m.input.Fields[1].Value = "/long/界界/path/with/control\x1b[31m"
			m.directories.suggestions = directory.Suggestions{Paths: []string{"api/", "app/"}, Incomplete: true}
			m.err = errors.New("invalid directory")

			view := m.viewForm(size.width, size.height)

			require.LessOrEqual(t, lipgloss.Height(view), size.height)
			for line := range strings.SplitSeq(view, "\n") {
				require.LessOrEqual(t, lipgloss.Width(line), size.width)
			}
			require.Contains(t, terminal.Sanitize(view), "inval")
			require.NotContains(t, view, "\x1b[31m")
		})
	}
}

func TestShortFormShowsOmittedDirectoriesAlongsideSelectedSuggestion(t *testing.T) {
	for _, test := range []struct {
		name       string
		height     int
		paths      []string
		incomplete bool
		want       string
	}{
		{name: "one row", height: 9, paths: []string{"api/", "app/"}, incomplete: true, want: "More omitted | > api/"},
		{name: "two rows", height: 10, paths: []string{"api/", "app/"}, incomplete: true, want: "More folders omitted"},
		{name: "scan limit without matches", height: 9, incomplete: true, want: "More omitted"},
		{name: "complete results", height: 9, paths: []string{"api/"}, want: "> api/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := formModel(t, nil)
			m.input.Focus = 1
			m.directories.suggestions = directory.Suggestions{Paths: test.paths, Incomplete: test.incomplete}

			view := m.viewForm(80, test.height)
			text := terminal.Sanitize(view)

			require.Contains(t, text, test.want)
			require.LessOrEqual(t, lipgloss.Height(view), test.height)
			if len(test.paths) > 0 {
				require.Contains(t, text, "> api/")
			}
			if !test.incomplete {
				require.NotContains(t, text, "omitted")
			}
		})
	}
}
