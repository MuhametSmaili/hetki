package state

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareIgnoresLayoutPaneIDsButKeepsGeometry(t *testing.T) {
	legacy := "0c68,80x24,0,0{78x24,0,0,1,1x24,79,0[1x12,79,0,2,1x11,79,13,3]}"
	jsonLayout := `{"V":2,"L":{"t":"h","w":80,"h":24,"x":0,"y":0,"c":[{"t":"p","w":78,"h":24,"x":0,"y":0,"i":0,"I":"%4","a":true},{"t":"v","w":1,"h":24,"x":79,"y":0,"c":[{"t":"p","w":1,"h":12,"x":79,"y":0,"i":1,"I":"%5"},{"t":"p","w":1,"h":11,"x":79,"y":13,"i":2,"I":"%6","l":0}]}]}}`
	for _, tc := range []struct {
		name   string
		layout string
		match  bool
	}{
		{"new legacy pane IDs", "e46e,80x24,0,0{78x24,0,0,4,1x24,79,0[1x12,79,0,5,1x11,79,13,6]}", true},
		{"tmux 3.8 JSON", jsonLayout, true},
		{"valid pane geometry changed", `{"V":2,"L":{"t":"h","w":80,"h":24,"x":0,"y":0,"c":[{"t":"p","w":77,"h":24,"x":0,"y":0,"i":0,"I":"%4"},{"t":"v","w":2,"h":24,"x":78,"y":0,"c":[{"t":"p","w":2,"h":12,"x":78,"y":0,"i":1,"I":"%5"},{"t":"p","w":2,"h":11,"x":78,"y":13,"i":2,"I":"%6"}]}]}}`, false},
		{"pane position changed", strings.Replace(jsonLayout, `"y":13`, `"y":12`, 1), false},
		{"split direction changed", strings.Replace(jsonLayout, `"t":"v"`, `"t":"h"`, 1), false},
		{"missing coordinate is not zero", strings.Replace(jsonLayout, `"x":0,`, "", 1), false},
		{"null metadata is not absence", strings.Replace(jsonLayout, `"i":0`, `"i":null`, 1), false},
		{"duplicate keys are ambiguous", strings.Replace(jsonLayout, `"w":80`, `"w":80,"w":80`, 1), false},
		{"document alias is not an exact key", strings.Replace(jsonLayout, `"L":`, `"l":`, 1), false},
		{"document aliases cannot merge", strings.Replace(jsonLayout, `"V":2`, `"V":2,"l":{}`, 1), false},
		{"cell alias is not an exact key", strings.Replace(jsonLayout, `"l":0`, `"L":0`, 1), false},
		{"case aliases are not exact keys", strings.Replace(jsonLayout, `"w":80`, `"W":80`, 1), false},
		{"floating pane is unsupported", strings.Replace(jsonLayout, `"i":0`, `"z":0,"i":0`, 1), false},
		{"unknown layout data", strings.Replace(jsonLayout, `"w":80`, `"future":true,"w":80`, 1), false},
		{"truncated JSON", jsonLayout[:len(jsonLayout)-1], false},
		{"unknown format version", strings.Replace(jsonLayout, `"V":2`, `"V":3`, 1), false},
		{"invalid checksum", strings.Replace(legacy, "0c68", "0000", 1), false},
		{"oversized layout", jsonLayout + strings.Repeat(" ", 16<<10), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := NewState()
			desired.AddSession("dev").Windows = []*Window{{Name: "editor", Path: "/repo", Layout: legacy}}
			desired.Sessions["dev"].Windows[0].Panes = []*Pane{{Path: "/repo"}, {Path: "/repo"}, {Path: "/repo"}}
			actual := NewState()
			actual.AddSession("dev").Windows = []*Window{{Name: "editor", Path: "/repo", Layout: tc.layout}}

			actual.Sessions["dev"].Windows[0].Panes = desired.Sessions["dev"].Windows[0].Panes

			diff := Compare(desired, actual)

			assert.Equal(t, tc.match, diff.Windows["dev"].IsEmpty())
		})
	}
}
