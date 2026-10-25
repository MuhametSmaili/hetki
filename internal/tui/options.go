package tui

import (
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// ViewMode identifies a projection even when it contains no items.
type ViewMode string

const (
	ViewFlat ViewMode = "flat"
	ViewTree ViewMode = "tree"
)

// Validate rejects missing and unknown projection modes.
func (v ViewMode) Validate() error {
	if v != ViewFlat && v != ViewTree {
		return fmt.Errorf("invalid view mode %q", v)
	}
	return nil
}

// Scope selects which items are included in the picker.
type Scope string

const (
	ScopeAll     Scope = "all"
	ScopeTracked Scope = "tracked"
)

// Validate rejects missing and unknown scopes.
func (s Scope) Validate() error {
	if s != ScopeAll && s != ScopeTracked {
		return fmt.Errorf("invalid scope %q", s)
	}
	return nil
}

// State contains the user's current UI choices.
type State struct {
	View    ViewMode
	Scope   Scope
	Preview bool
}

// Options contains fully resolved startup settings, consumed once per run.
type Options struct {
	Initial      State
	Keys         KeyMap
	StartMode    StartMode
	PreviewWidth int
}

// Runtime contains effects owned, canceled and joined by the caller.
type Runtime struct {
	Dispatch DispatchFunc
	Preview  PreviewFunc
	Status   StatusOptions
}

// Result contains the final choices and an optional navigation destination.
type Result struct {
	Navigation list.ItemID
	State      State
}

// DefaultOptions returns the complete built-in picker settings.
func DefaultOptions() Options {
	return Options{
		Initial:      State{View: ViewFlat, Scope: ScopeAll, Preview: false},
		Keys:         DefaultKeyMap(),
		StartMode:    DefaultStartMode(),
		PreviewWidth: DefaultPreviewWidth,
	}
}

// Validate checks resolved programmatic settings without supplying defaults.
func (o Options) Validate() error {
	if err := o.Initial.View.Validate(); err != nil {
		return err
	}
	if err := o.Initial.Scope.Validate(); err != nil {
		return err
	}
	if o.Keys.IsZero() {
		return fmt.Errorf("keymap is not configured")
	}
	if o.StartMode != StartModeNormal && o.StartMode != StartModeFilter {
		return fmt.Errorf("invalid start mode %q", o.StartMode)
	}
	if o.PreviewWidth < 1 || o.PreviewWidth > 99 {
		return fmt.Errorf("preview width must be between 1 and 99 percent")
	}
	return nil
}
