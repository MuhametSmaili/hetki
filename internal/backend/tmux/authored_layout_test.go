package tmux

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAuthoredLayoutPreservesAcceptedSyntax(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")

	for _, text := range []string{
		"", "even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled",
		"bb62,159x48,0,0,0",
		"2419,80x24,0,0{40x24,0,0,0,39x24,41,0,1}",
		"cafe,80x24,0,0{40x12,0,0,0,40x11,0,13,1,39x23,41,0,2}",
		"cafe,80x24,0,0[80x12,0,0{40x12,0,0,0,39x12,41,0,1},80x11,0,13,2]",
	} {
		t.Run(text, func(t *testing.T) {
			err := ValidateAuthoredLayout(text)

			require.NoError(t, err)
		})
	}
}

func TestAuthoredAcceptanceDoesNotRelaxObservationChecks(t *testing.T) {
	deep := "1x1,0,0,0"
	for range 40 {
		deep = "1x1,0,0[" + deep + ",1x1,0,0,1]"
	}
	backend := &TmuxBackend{}

	for _, tc := range []struct {
		name, text string
	}{
		{"unchecked checksum", "cafe,80x24,0,0,0"},
		{"zero dimensions", "c05d,0x0,0,0,0"},
		{"integer overflow", "fe7d," + strings.Repeat("9", 100) + "x1,0,0,0"},
		{"nesting exceeds observation limit", "4e7a," + deep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authoredErr := ValidateAuthoredLayout(tc.text)
			preset, observedErr := backend.LayoutPreset(tc.text)

			require.NoError(t, authoredErr)
			require.Error(t, observedErr)
			require.Empty(t, preset)
		})
	}
}

func TestValidateAuthoredLayoutRejectsInvalidSyntax(t *testing.T) {
	for _, text := range []string{
		"diagonal", " tiled ",
		"zzzz,80x24,0,0,0",
		"cafe,80x24,0,0{40x24,0,0,0}",
		"cafe,80x24,0,0[40x12,0,0,0,39x12,41,0,1}",
		"cafe,80x24,0,0,0 trailing",
		`{"V":2,"L":{"t":"p","w":80,"h":24,"x":0,"y":0,"I":"%0"}}`,
		"cafe," + strings.Repeat("9", 16<<10) + "x1,0,0,0",
	} {
		t.Run(text[:min(len(text), 80)], func(t *testing.T) {
			err := ValidateAuthoredLayout(text)

			require.ErrorContains(t, err, "unknown layout")
			if len(text) > 16<<10 {
				require.LessOrEqual(t, len(err.Error()), 80, "oversized input must not be echoed")
			}
		})
	}
}
