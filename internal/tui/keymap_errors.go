package tui

import "errors"

// Keymap validation categories have fixed messages safe for user-facing warnings.
// Resolvers can wrap them with supplied values; inspect categories with errors.Is
// instead of displaying those detailed errors.
var (
	ErrUnknownKeyMode       = errors.New("unsupported mode")
	ErrUnsupportedKeyAction = errors.New("unsupported action for this mode")
	ErrInvalidKeyCount      = errors.New("each action requires 1–16 keys")
	ErrInvalidKeySyntax     = errors.New("invalid key syntax")
	ErrReservedQuitKey      = errors.New("ctrl+c is reserved for emergency quit")
	ErrKeyBindingConflict   = errors.New("conflicting or duplicate bindings")
)
