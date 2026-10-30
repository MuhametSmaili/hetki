package tui

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
)

// WithOverrides replaces existing mode/action key lists without modifying k.
// Omitted actions retain their mode-local defaults. Empty lists and same-mode
// key conflicts are rejected so required controls remain usable.
func (k KeyMap) WithOverrides(overrides map[KeyMode][]Binding) (KeyMap, error) {
	if len(overrides) == 0 {
		return k, nil
	}

	resolved := KeyMap{bindings: maps.Clone(k.bindings)}
	for mode, bindings := range overrides {
		defaults, exists := k.bindings[mode]
		if !exists {
			return KeyMap{}, fmt.Errorf("invalid key mode %q: %w", mode, ErrUnknownKeyMode)
		}
		if len(bindings) == 0 {
			continue
		}

		actions, err := overrideModeBindings(mode, defaults, bindings)
		if err != nil {
			return KeyMap{}, err
		}
		resolved.bindings[mode] = actions
	}

	return resolved, nil
}

func overrideModeBindings(mode KeyMode, defaults map[ActionID]key.Binding, overrides []Binding) (map[ActionID]key.Binding, error) {
	merged := make([]Binding, 0, len(defaults)+len(overrides))
	replaced := make(map[ActionID]bool, len(overrides))
	for _, binding := range overrides {
		if _, exists := defaults[binding.Action]; !exists {
			return nil, fmt.Errorf("mode %q has no action %q: %w", mode, binding.Action, ErrUnsupportedKeyAction)
		}
		if err := validateOverrideKeys(binding.Keys); err != nil {
			return nil, fmt.Errorf("mode %q action %q: %w", mode, binding.Action, err)
		}

		merged = append(merged, binding)
		replaced[binding.Action] = true
	}
	for action, binding := range defaults {
		if !replaced[action] {
			merged = append(merged, Binding{Action: action, Keys: binding.Keys()})
		}
	}
	return resolveModeBindings(mode, merged)
}

func validateOverrideKeys(keys []string) error {
	if len(keys) == 0 || len(keys) > 16 {
		return ErrInvalidKeyCount
	}
	for index, value := range keys {
		if !validOverrideKey(value) {
			return fmt.Errorf("key %d %q: %w", index+1, value, ErrInvalidKeySyntax)
		}
	}
	return nil
}

// Match the canonical strings used by KeyPressMsg.String, not key aliases or
// chords. Shifted printable keys use their literal character, such as G or ?.
func validOverrideKey(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}

	base := value
	var modified, shift bool
	for _, prefix := range []string{"ctrl+", "alt+", "shift+"} {
		if rest, found := strings.CutPrefix(base, prefix); found {
			base = rest
			modified = true
			shift = prefix == "shift+"
		}
	}
	if utf8.RuneCountInString(base) == 1 {
		r, _ := utf8.DecodeRuneInString(base)
		return unicode.IsPrint(r) && !unicode.IsSpace(r) &&
			(!modified || !unicode.IsUpper(r)) &&
			(!shift || strings.HasPrefix(value, "ctrl+") || strings.HasPrefix(value, "alt+"))
	}
	return namedOverrideKey(base)
}

func namedOverrideKey(value string) bool {
	switch value {
	case "enter", "esc", "space", "tab", "backspace", "delete",
		"up", "down", "left", "right", "home", "end", "insert", "pgup", "pgdown":
		return true
	}
	if !strings.HasPrefix(value, "f") {
		return false
	}
	number, err := strconv.Atoi(value[1:])
	return err == nil && number >= 1 && number <= 24 && value == "f"+strconv.Itoa(number)
}
