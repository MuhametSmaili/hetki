package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayoutPresetRecognizesReadableShapes(t *testing.T) {
	b := &TmuxBackend{}
	for _, tc := range []struct {
		name   string
		layout string
		preset string
	}{
		{"side by side", "f184,120x60,0,0{40x60,0,0,0,39x60,41,0,1,39x60,81,0,2}", "even-horizontal"},
		{"stacked JSON", `{"V":2,"L":{"t":"v","w":120,"h":60,"x":0,"y":0,"c":[{"t":"p","w":120,"h":20,"x":0,"y":0,"i":0,"I":"%0"},{"t":"p","w":120,"h":19,"x":0,"y":21,"i":1,"I":"%1"},{"t":"p","w":120,"h":19,"x":0,"y":41,"i":2,"I":"%2"}]}}`, "even-vertical"},
		{"main above", "0b8e,120x60,0,0[120x24,0,0,0,120x35,0,25{60x35,0,25,1,59x35,61,25,2}]", "main-horizontal"},
		{"main left", "68c3,120x60,0,0{80x60,0,0,0,39x60,81,0[39x30,81,0,1,39x29,81,31,2]}", "main-vertical"},
		{"mixed JSON", `{"V":2,"L":{"t":"v","w":120,"h":60,"x":0,"y":0,"c":[{"t":"h","w":120,"h":29,"x":0,"y":0,"c":[{"t":"p","w":59,"h":29,"x":0,"y":0,"i":0,"I":"%0"},{"t":"p","w":60,"h":29,"x":60,"y":0,"i":1,"I":"%1"}]},{"t":"p","w":120,"h":30,"x":0,"y":30,"i":2,"I":"%2"}]}}`, "tiled"},
		{"already named", "main-vertical", "main-vertical"},
		{"unobserved", "", ""},
		{"single pane", "b25d,80x24,0,0,0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preset, err := b.LayoutPreset(tc.layout)

			require.NoError(t, err)
			assert.Equal(t, tc.preset, preset)
		})
	}
}
