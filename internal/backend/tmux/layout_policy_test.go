package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLayoutsMatchKeepsPresetRequestsBestEffort(t *testing.T) {
	b := &TmuxBackend{}
	for _, request := range []string{"", "even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled"} {
		t.Run(request, func(t *testing.T) {
			match := b.LayoutsMatch(request, "opaque observation")

			assert.True(t, match)
		})
	}
}
