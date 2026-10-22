package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type KeyMode string

const (
	KeyModeNormal  KeyMode = "normal"
	KeyModeFilter  KeyMode = "filter"
	KeyModeInput   KeyMode = "input"
	KeyModeConfirm KeyMode = "confirm"
	KeyModeMenu    KeyMode = "menu"
)

type Binding struct {
	Action ActionID
	Keys   []string
}

type KeyMap struct {
	bindings map[KeyMode]map[ActionID]key.Binding
}

func ResolveKeyMap(bindings map[KeyMode][]Binding) (KeyMap, error) {
	resolved := KeyMap{bindings: make(map[KeyMode]map[ActionID]key.Binding, len(bindings))}
	for mode, modeBindings := range bindings {
		switch mode {
		case KeyModeNormal, KeyModeFilter, KeyModeInput, KeyModeConfirm, KeyModeMenu:
		default:
			return KeyMap{}, fmt.Errorf("invalid key mode %q", mode)
		}

		actions, err := resolveModeBindings(mode, modeBindings)
		if err != nil {
			return KeyMap{}, err
		}
		resolved.bindings[mode] = actions
	}
	return resolved, nil
}

func resolveModeBindings(mode KeyMode, bindings []Binding) (map[ActionID]key.Binding, error) {
	actions := make(map[ActionID]key.Binding, len(bindings))
	used := make(map[string]ActionID)
	for _, binding := range bindings {
		if binding.Action == "" || len(binding.Keys) == 0 {
			return nil, fmt.Errorf("mode %q has an empty action or key list", mode)
		}

		if !slices.Contains(declaredActions[:], binding.Action) {
			return nil, fmt.Errorf("mode %q has an invalid action %q", mode, binding.Action)
		}

		if _, exists := actions[binding.Action]; exists {
			return nil, fmt.Errorf("mode %q defines action %q more than once", mode, binding.Action)
		}
		keys, err := resolveBindingKeys(mode, binding, used)
		if err != nil {
			return nil, err
		}
		actions[binding.Action] = key.NewBinding(key.WithKeys(keys...))
	}
	return actions, nil
}

func resolveBindingKeys(mode KeyMode, binding Binding, used map[string]ActionID) ([]string, error) {
	keys := make([]string, len(binding.Keys))
	for i, raw := range binding.Keys {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, fmt.Errorf("mode %q action %q has an empty key", mode, binding.Action)
		}
		if value == "ctrl+c" {
			return nil, fmt.Errorf("ctrl+c is reserved for emergency quit")
		}
		if previous, exists := used[value]; exists {
			return nil, fmt.Errorf("mode %q key %q is assigned to %q and %q", mode, value, previous, binding.Action)
		}
		used[value] = binding.Action
		keys[i] = value
	}
	return keys, nil
}

func DefaultKeyMap() KeyMap {
	keymap, err := ResolveKeyMap(map[KeyMode][]Binding{
		KeyModeNormal: {
			{Action: ActionTogglePreview, Keys: []string{"alt+p"}},
			{Action: ActionQuit, Keys: []string{"q"}},
			{Action: ActionMoveUp, Keys: []string{"up", "k", "ctrl+p"}},
			{Action: ActionMoveDown, Keys: []string{"down", "j", "ctrl+n"}},
			{Action: ActionMoveTop, Keys: []string{"g"}},
			{Action: ActionMoveBottom, Keys: []string{"G"}},
			{Action: ActionPageUp, Keys: []string{"pgup", "u"}},
			{Action: ActionPageDown, Keys: []string{"pgdown", "d"}},
			{Action: ActionFilter, Keys: []string{"/"}},
			{Action: ActionNextMatch, Keys: []string{"n"}},
			{Action: ActionPrevMatch, Keys: []string{"N"}},
			{Action: ActionClearFilter, Keys: []string{"ctrl+l"}},
			{Action: ActionContextMenu, Keys: []string{"ctrl+k"}},
			{Action: ActionCreateSession, Keys: []string{"A"}},
			{Action: ActionCreateWindow, Keys: []string{"a"}},
			{Action: ActionRename, Keys: []string{"r"}},
			{Action: ActionRenameSession, Keys: []string{"R"}},
			{Action: ActionDelete, Keys: []string{"x"}},
			{Action: ActionDeleteSession, Keys: []string{"X"}},
			{Action: ActionExpand, Keys: []string{"right", "l"}},
			{Action: ActionCollapse, Keys: []string{"left", "h"}},
			{Action: ActionExpandAll, Keys: []string{"L"}},
			{Action: ActionCollapseAll, Keys: []string{"H"}},
			{Action: ActionRefresh, Keys: []string{"ctrl+r"}},
			{Action: ActionToggleProjection, Keys: []string{"ctrl+t"}},
			{Action: ActionToggleScope, Keys: []string{"tab"}},
			{Action: ActionOpen, Keys: []string{"enter", "ctrl+y"}},
			{Action: ActionLastSession, Keys: []string{"ctrl+o"}},
		},
		KeyModeFilter: {
			{Action: ActionToggleProjection, Keys: []string{"ctrl+t"}},
			{Action: ActionToggleScope, Keys: []string{"tab"}},
			{Action: ActionTogglePreview, Keys: []string{"alt+p"}},
			{Action: ActionLastSession, Keys: []string{"ctrl+o"}},
			{Action: ActionCancel, Keys: []string{"esc"}},
			{Action: ActionConfirm, Keys: []string{"enter", "ctrl+y"}},
			{Action: ActionMoveUp, Keys: []string{"up", "ctrl+p"}},
			{Action: ActionMoveDown, Keys: []string{"down", "ctrl+n"}},
			{Action: ActionPageUp, Keys: []string{"pgup"}},
			{Action: ActionPageDown, Keys: []string{"pgdown"}},
			{Action: ActionBackspace, Keys: []string{"backspace", "delete"}},
			{Action: ActionDeleteWord, Keys: []string{"ctrl+w"}},
			{Action: ActionDeleteToStart, Keys: []string{"ctrl+u"}},
			{Action: ActionClearFilter, Keys: []string{"ctrl+l"}},
			{Action: ActionContextMenu, Keys: []string{"ctrl+k"}},
		},
		KeyModeInput: {
			{Action: ActionCancel, Keys: []string{"esc"}},
			{Action: ActionConfirm, Keys: []string{"enter", "ctrl+y"}},
			{Action: ActionBackspace, Keys: []string{"backspace", "delete"}},
			{Action: ActionDeleteWord, Keys: []string{"ctrl+w"}},
			{Action: ActionDeleteToStart, Keys: []string{"ctrl+u"}},
		},
		KeyModeConfirm: {
			{Action: ActionCancel, Keys: []string{"esc", "n", "N"}},
			{Action: ActionConfirm, Keys: []string{"enter", "ctrl+y", "y", "Y"}},
		},
		KeyModeMenu: {
			{Action: ActionOpen, Keys: []string{"o"}},
			{Action: ActionCancel, Keys: []string{"esc"}},
			{Action: ActionConfirm, Keys: []string{"enter", "ctrl+y"}},
			{Action: ActionMoveUp, Keys: []string{"up", "k", "ctrl+p"}},
			{Action: ActionMoveDown, Keys: []string{"down", "j", "ctrl+n"}},
		},
	})
	if err != nil {
		panic(err)
	}
	return keymap
}

// Menu item shortcuts inherit normal bindings; menu controls and Open are local.
func (k KeyMap) menuMode(action ActionID) KeyMode {
	if _, exists := k.bindings[KeyModeMenu][action]; exists {
		return KeyModeMenu
	}
	return KeyModeNormal
}

func (k KeyMap) IsZero() bool { return k.bindings == nil }

func (k KeyMap) Keys(mode KeyMode, action ActionID) []string {
	binding, ok := k.bindings[mode][action]
	if !ok {
		return nil
	}
	return append([]string(nil), binding.Keys()...)
}

func (k KeyMap) Matches(mode KeyMode, action ActionID, msg tea.KeyPressMsg) bool {
	binding, ok := k.bindings[mode][action]
	return ok && key.Matches(msg, binding)
}

func (k KeyMap) Action(mode KeyMode, msg tea.KeyPressMsg) (ActionID, bool) {
	for action, binding := range k.bindings[mode] {
		if key.Matches(msg, binding) {
			return action, true
		}
	}
	return "", false
}
