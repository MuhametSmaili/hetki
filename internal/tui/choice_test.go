package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func choiceResult(key, label string) ActionResult {
	snapshot := list.Snapshot{Items: []list.Item{{
		ID: list.ItemID{Scope: "actions", Parent: key, ID: "choice"}, Primary: label,
		SearchFields: []list.SearchField{{Tier: list.SearchPrimary, Text: label}},
	}}}
	return ActionResult{Choice: &ChoiceScreen{Key: key, Title: label}, Snapshot: &snapshot}
}

func TestInitialChoiceAlwaysAcceptsFilterText(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot list.Snapshot
	}{
		{"populated", *choiceResult("root", "Join pane").Snapshot},
		{"empty", list.Snapshot{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := DefaultOptions()
			options.StartMode = StartModeNormal
			options.Choice = &ChoiceScreen{Key: "root", Title: "Actions"}
			m, err := newModelWithOptions(t.Context(), test.snapshot, options, Runtime{})
			require.NoError(t, err)

			m, _ = updateModel(t, m, printableKey("j"))

			assert.Equal(t, modeFilter, m.mode)
			assert.Equal(t, "j", m.items.Query())
		})
	}
}

func TestMenuShortcutOpensUserActions(t *testing.T) {
	for _, test := range []struct {
		name      string
		key       string
		overrides map[KeyMode][]Binding
	}{
		{name: "default", key: "U"},
		{name: "configured", key: "z", overrides: map[KeyMode][]Binding{
			KeyModeMenu: {{Action: ActionUserActions, Keys: []string{"z"}}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys, err := DefaultKeyMap().WithOverrides(test.overrides)
			require.NoError(t, err)
			var got ActionRequest
			m, err := newModelWithKeys(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
				if request.ActionID == ActionContextMenu {
					return ActionResult{Menu: &ItemMenu{Entries: []MenuEntry{
						{Action: ActionOpen, Label: "Open"},
						{Action: ActionUserActions, Label: "User actions…"},
					}}}, nil
				}
				got = request
				return choiceResult("actions", "Join pane"), nil
			}, keys)
			require.NoError(t, err)
			subject := selectedNodeID(m)
			m = openTestMenu(t, m)

			m, cmd := updateModel(t, m, printableKey(test.key))
			require.NotNil(t, cmd)
			m, _ = updateModel(t, m, cmd())

			require.NoError(t, m.err)
			assert.Equal(t, ActionRequest{ActionID: ActionUserActions, ItemID: subject}, got)
			require.NotNil(t, m.choice)
			assert.Equal(t, "actions", m.choice.screen.Key)
			assert.Equal(t, "Join pane", m.items.Rows()[0].Item.Primary)
			assert.Equal(t, modeFilter, m.mode)
		})
	}
}

func TestChoiceScreensOwnQueriesAndBackRestoresSelection(t *testing.T) {
	menu := choiceResult("custom-root", "Join pane")
	menu.Choice.ResetHistory = true
	destination := choiceResult("join/0", "Editor window")
	var requests []ActionRequest
	dispatch := func(request ActionRequest) (ActionResult, error) {
		requests = append(requests, request)
		if request.Choice == ChoiceBack {
			return menu, nil
		}
		return destination, nil
	}
	options := DefaultOptions()
	options.Choice = menu.Choice
	m, err := newModelWithOptions(t.Context(), *menu.Snapshot, options, Runtime{Dispatch: dispatch})
	require.NoError(t, err)
	m.items.SetQuery("join")
	selected, _ := m.items.Selected()

	m, cmd := updateModel(t, m, specialKey(tea.KeyEnter))
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())
	assert.Empty(t, m.items.Query())
	assert.Equal(t, "join/0", m.choice.screen.Key)
	m.items.SetQuery("editor")
	m, cmd = updateModel(t, m, specialKey(tea.KeyEscape))
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())

	assert.Equal(t, "join", m.items.Query())
	row, ok := m.items.Selected()
	require.True(t, ok)
	assert.Equal(t, selected.Item.ID, row.Item.ID)
	require.Len(t, requests, 2)
	assert.Equal(t, ChoiceSelect, requests[0].Choice)
	assert.Equal(t, ChoiceBack, requests[1].Choice)

	m, cmd = updateModel(t, m, specialKey(tea.KeyEnter))
	require.NotNil(t, cmd)
	m, _ = updateModel(t, m, cmd())

	assert.Empty(t, m.items.Query(), "returning to the root clears downstream queries")
}

func TestChoiceHistoryResetRequiresExplicitSignal(t *testing.T) {
	menu := choiceResult("actions", "Join pane")
	m := newModel(interactionSnapshot(), nil)
	next, _ := m.applyChoiceResult(menu, nil)
	m = next.(model)
	m.choice.history["destination"] = choicePosition{query: "editor"}

	next, _ = m.applyChoiceResult(menu, nil)
	m = next.(model)

	assert.Equal(t, "editor", m.choice.history["destination"].query)
}

func TestChoiceKeyOverridesAndEscapePrecedence(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   tea.KeyPressMsg
		want  ChoiceEvent
		query string
	}{
		{"Escape always goes back", specialKey(tea.KeyEscape), ChoiceBack, "join"},
		{"configured cancel goes back", tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt}, ChoiceBack, "join"},
		{"configured Ctrl+R clears filter", controlKey('r'), "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{
				KeyModeFilter: {
					{Action: ActionCancel, Keys: []string{"alt+b"}},
					{Action: ActionMoveDown, Keys: []string{"esc"}},
					{Action: ActionClearFilter, Keys: []string{"ctrl+r"}},
				},
			})
			require.NoError(t, err)
			menu := choiceResult("root", "Join pane")
			options := DefaultOptions()
			options.Choice, options.Keys = menu.Choice, keys
			var event ChoiceEvent
			m, err := newModelWithOptions(t.Context(), *menu.Snapshot, options, Runtime{Dispatch: func(request ActionRequest) (ActionResult, error) {
				event = request.Choice
				return menu, nil
			}})
			require.NoError(t, err)
			m.items.SetQuery("join")

			m, cmd := updateModel(t, m, test.key)
			if cmd != nil {
				cmd()
			}

			assert.Equal(t, test.want, event)
			assert.Equal(t, test.query, m.items.Query())
		})
	}
}

func TestChoiceFailureReplacesOldCandidates(t *testing.T) {
	menu := choiceResult("actions", "Join pane")
	options := DefaultOptions()
	options.Choice = menu.Choice
	m, err := newModelWithOptions(t.Context(), *menu.Snapshot, options, Runtime{})
	require.NoError(t, err)
	failed := ActionResult{Choice: &ChoiceScreen{Key: "join/0", Title: "Windows"}, Snapshot: &list.Snapshot{}}

	next, cmd := m.handleActionResult(actionResultMsg{result: failed, err: errors.New("provider failed")})
	m = next.(model)

	assert.Nil(t, cmd)
	assert.Empty(t, m.items.Rows())
	assert.Equal(t, "join/0", m.choice.screen.Key)
	require.ErrorContains(t, m.err, "provider failed")
}

func TestChoiceEmptyStatesDistinguishLoadingAndFiltering(t *testing.T) {
	for _, test := range []struct {
		name      string
		populated bool
		query     string
		loadErr   error
		want      string
	}{
		{name: "empty source", want: "no choices available · ctrl+r refresh · Esc back"},
		{name: "empty source with query", query: "missing", want: "no choices available · ctrl+r refresh · Esc back"},
		{name: "failed source", loadErr: errors.New("provider exited 7"), want: "could not load choices · ctrl+r retry · Esc back"},
		{name: "failed source with query", query: "missing", loadErr: errors.New("provider exited 7"), want: "could not load choices · ctrl+r retry · Esc back"},
		{name: "filter has no matches", populated: true, query: "missing", want: "no matching choices · ctrl+l clear filter · Esc back"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := choiceResult("choices", "Editor")
			if !test.populated {
				result.Snapshot = &list.Snapshot{}
			}
			m := newModel(interactionSnapshot(), nil)
			next, _ := m.handleActionResult(actionResultMsg{result: result, err: test.loadErr})
			m = next.(model)
			m.setQuery(test.query)
			m.width, m.height = 120, 24
			m = m.reflow()

			content := terminal.Sanitize(m.View().Content)

			assert.Contains(t, content, test.want)
			assert.NotContains(t, content, "no sessions found")
			if test.loadErr != nil {
				assert.Contains(t, content, test.loadErr.Error())
			}
			_, cmd := updateModel(t, m, specialKey(tea.KeyEnter))
			assert.Nil(t, cmd, "an empty list cannot submit a choice")
		})
	}
}

func TestChoiceRefreshRecoversAndClearKeepsFiltering(t *testing.T) {
	var events []ChoiceEvent
	m := newModel(interactionSnapshot(), func(request ActionRequest) (ActionResult, error) {
		events = append(events, request.Choice)
		return choiceResult("choices", "Editor"), nil
	})
	failed := ActionResult{Choice: &ChoiceScreen{Key: "choices", Title: "Windows"}, Snapshot: &list.Snapshot{}}
	next, _ := m.handleActionResult(actionResultMsg{result: failed, err: errors.New("provider failed")})
	m = next.(model)
	m.setQuery("missing")

	m, cmd := updateModel(t, m, controlKey('r'))
	require.NotNil(t, cmd)
	assert.Equal(t, "loading choices…", emptyStateText(m))
	m, _ = updateModel(t, m, cmd())

	require.NoError(t, m.err)
	assert.Equal(t, []ChoiceEvent{ChoiceRefresh}, events)
	assert.Contains(t, emptyStateText(m), "no matching choices")
	m, cmd = updateModel(t, m, controlKey('l'))
	assert.Nil(t, cmd)
	m, _ = updateModel(t, m, printableKey("editor"))
	assert.Equal(t, "editor", m.items.Query())
	assert.Equal(t, modeFilter, m.mode)
	require.Len(t, m.items.Rows(), 1)
	assert.Equal(t, "Editor", m.items.Rows()[0].Item.Primary)
	assert.Len(t, events, 1, "editing the filter must not reload the provider")
}

func TestChoiceRecoveryHintsRespectKeyOverrides(t *testing.T) {
	for _, test := range []struct {
		name     string
		bindings []Binding
		query    string
		want     string
		absent   string
	}{
		{name: "Ctrl+R assigned elsewhere", bindings: []Binding{{Action: ActionClearFilter, Keys: []string{"ctrl+r"}}}, want: "Esc back", absent: "ctrl+r refresh"},
		{name: "custom clear", bindings: []Binding{{Action: ActionClearFilter, Keys: []string{"alt+l"}}}, query: "missing", want: "alt+l clear filter", absent: "ctrl+l clear filter"},
		{name: "reserved Escape does not clear", bindings: []Binding{{Action: ActionCancel, Keys: []string{"alt+b"}}, {Action: ActionClearFilter, Keys: []string{"esc"}}}, query: "missing", want: "edit filter", absent: "esc clear filter"},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys, err := DefaultKeyMap().WithOverrides(map[KeyMode][]Binding{KeyModeFilter: test.bindings})
			require.NoError(t, err)
			result := choiceResult("choices", "Editor")
			if test.query == "" {
				result.Snapshot = &list.Snapshot{}
			}
			options := DefaultOptions()
			options.Choice, options.Keys = result.Choice, keys
			m, err := newModelWithOptions(t.Context(), *result.Snapshot, options, Runtime{})
			require.NoError(t, err)
			m.setQuery(test.query)

			message := emptyStateText(m)

			assert.Contains(t, message, test.want)
			assert.NotContains(t, message, test.absent)
		})
	}
}

func TestChoiceControlsDoNotDispatchNavigationMutations(t *testing.T) {
	menu := choiceResult("actions", "Join pane")
	options := DefaultOptions()
	options.Choice = menu.Choice
	dispatched := false
	m, err := newModelWithOptions(t.Context(), *menu.Snapshot, options, Runtime{Dispatch: func(ActionRequest) (ActionResult, error) {
		dispatched = true
		return ActionResult{}, nil
	}})
	require.NoError(t, err)

	for _, action := range []ActionID{ActionDelete, ActionCreateSession, ActionToggleProjection, ActionToggleScope, ActionTogglePreview} {
		next, cmd := m.dispatchAction(action, list.ItemID{})
		assert.Nil(t, cmd)
		require.Error(t, next.(model).err)
	}

	assert.False(t, dispatched)
}

func TestChoiceRefreshWorksInFilterMode(t *testing.T) {
	menu := choiceResult("actions", "Join pane")
	options := DefaultOptions()
	options.Choice = menu.Choice
	var event ChoiceEvent
	m, err := newModelWithOptions(t.Context(), *menu.Snapshot, options, Runtime{Dispatch: func(request ActionRequest) (ActionResult, error) {
		event = request.Choice
		return menu, nil
	}})
	require.NoError(t, err)
	require.Equal(t, modeFilter, m.mode)

	_, cmd := updateModel(t, m, controlKey('r'))
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, ChoiceRefresh, event)
}

func TestEmbeddedChoiceReturnsToOriginalNavigationState(t *testing.T) {
	m := newModel(interactionSnapshot(), nil)
	m.items.SetQuery("server")
	m.mode = modeFilter
	m.preview.enabled = true
	before := m.items.Snapshot()

	next, _ := m.applyChoiceResult(choiceResult("actions", "Join pane"), nil)
	m = next.(model)
	assert.False(t, m.preview.enabled)
	next, cmd := m.applyChoiceResult(ActionResult{Choice: &ChoiceScreen{Closed: true}}, nil)
	m = next.(model)

	assert.Nil(t, cmd)
	assert.Nil(t, m.choice)
	assert.Equal(t, before, m.items.Snapshot())
	assert.Equal(t, "server", m.items.Query())
	assert.Equal(t, modeFilter, m.mode)
	assert.True(t, m.preview.enabled)
}
