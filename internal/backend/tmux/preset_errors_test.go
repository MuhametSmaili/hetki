package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayoutPresetRejectsInvalidAndUnsupportedObservations(t *testing.T) {
	b := &TmuxBackend{}
	for _, observed := range []string{
		"future-layout",
		`{"V":2,"L":{"t":"p","w":80,"h":24,"x":0,"y":0,"z":0}}`,
	} {
		t.Run(observed, func(t *testing.T) {
			preset, err := b.LayoutPreset(observed)

			require.Error(t, err)
			assert.Empty(t, preset)
		})
	}
}
