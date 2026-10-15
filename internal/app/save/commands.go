package save

import (
	"errors"
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/converter"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/MuhametSmaili/hetki/internal/state"
)

// ErrCommandPreservation means saving would lose or ambiguously assign an
// authored startup command. The destination is not replaced.
var ErrCommandPreservation = errors.New("cannot safely preserve startup commands")

func preserveCommands(existing, incoming *manifest.Workspace, validateLayout func(string) error) error {
	normalized, err := manifest.Normalize(incoming, validateLayout)
	if err != nil {
		return err
	}
	saved := converter.ManifestToState(existing)
	observed := converter.ManifestToState(normalized)

	for i := range incoming.Sessions {
		session := &incoming.Sessions[i]
		source, found := saved.Sessions[session.Name]
		if !found {
			continue
		}
		if err := preserveSessionCommands(source, observed.Sessions[session.Name], session.Windows); err != nil {
			return err
		}
	}
	return nil
}

func preserveSessionCommands(saved, observed *state.Session, windows []manifest.Window) error {
	for _, source := range saved.Windows {
		if !hasWindowCommands(source) {
			continue
		}
		_, savedCount := correspondingWindows(source, saved.Windows)
		index, observedCount := correspondingWindows(source, observed.Windows)
		if savedCount != 1 || observedCount != 1 {
			return fmt.Errorf("%w: session %q, window %q has %d saved matches and %d live matches; edit the destination or save to a new file",
				ErrCommandPreservation, saved.Name, source.Name, savedCount, observedCount)
		}
		copyWindowCommands(source, &windows[index])
	}
	return nil
}

func hasWindowCommands(window *state.Window) bool {
	for _, pane := range window.Panes {
		if pane.Command != "" {
			return true
		}
	}
	return false
}

func correspondingWindows(source *state.Window, windows []*state.Window) (index, count int) {
	index = -1
	for i, window := range windows {
		if state.WindowsCorrespond(source, window) {
			index = i
			count++
		}
	}
	return index, count
}

func copyWindowCommands(source *state.Window, destination *manifest.Window) {
	if len(destination.Panes) == 0 {
		destination.Command = source.Panes[0].Command
		return
	}
	for i := range destination.Panes {
		destination.Panes[i].Command = source.Panes[i].Command
	}
}
