package tui

import (
	"errors"

	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

func resolveKeymap(preferences map[string]map[string][]string, options *ui.Options) []string {
	if len(preferences) == 0 {
		return nil
	}

	overrides := make(map[ui.KeyMode][]ui.Binding, len(preferences))
	for mode, actions := range preferences {
		bindings := make([]ui.Binding, 0, len(actions))
		for action, keys := range actions {
			bindings = append(bindings, ui.Binding{Action: ui.ActionID(action), Keys: keys})
		}
		overrides[ui.KeyMode(mode)] = bindings
	}
	keys, err := options.Keys.WithOverrides(overrides)
	if err != nil {
		return []string{keymapWarning(err)}
	}
	options.Keys = keys
	return nil
}

func keymapWarning(err error) string {
	problem := "invalid bindings or conflicts"
	for _, category := range []error{
		ui.ErrUnknownKeyMode,
		ui.ErrUnsupportedKeyAction,
		ui.ErrInvalidKeyCount,
		ui.ErrInvalidKeySyntax,
		ui.ErrReservedQuitKey,
		ui.ErrKeyBindingConflict,
	} {
		if errors.Is(err, category) {
			problem = category.Error()
			break
		}
	}
	return "keymap: " + problem + "; using default keymap"
}
