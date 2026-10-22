package tui

import (
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestScopeHeaderKeepsLabelsFixedAndEmphasizesActiveScope(t *testing.T) {
	for _, tc := range []struct {
		name         string
		previewWidth int
		metaRow      int
	}{
		{name: "full-width"},
		{name: "preview", previewWidth: 20, metaRow: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, now := scopeFixture(t, "flat")
			layout := layoutMetrics{listWidth: 80, previewWidth: tc.previewWidth}
			m.mode = modeFilter
			all := strings.Split(m.viewHeader(layout), "\n")[tc.metaRow]
			m.scope.tracked = true
			m.applyScope("idle", now)
			m.mode = modeJump

			lines := strings.Split(m.viewHeader(layout), "\n")
			tracked := lines[tc.metaRow]
			screen := uv.NewScreenBuffer(80, 1)
			uv.NewStyledString(tracked).Draw(screen, screen.Bounds())

			allText, trackedText := terminal.Sanitize(all), terminal.Sanitize(tracked)
			require.Contains(t, terminal.Sanitize(lines[0]), "JUMP")
			require.Contains(t, terminal.Sanitize(lines[0]), "idle")
			require.NotContains(t, trackedText, "Flat")
			require.NotContains(t, trackedText, "Tree")
			for _, label := range []string{"All", "Tracked"} {
				index := strings.Index(trackedText, label)
				require.GreaterOrEqual(t, index, 0)
				x := terminal.Width(trackedText[:index])
				require.Equal(t, terminal.Width(allText[:strings.Index(allText, label)]), x)
				cell := screen.CellAt(x, 0)
				if label == "Tracked" {
					require.NotZero(t, cell.Style.Underline)
				} else {
					require.Zero(t, cell.Style.Underline)
				}
			}
		})
	}
}

func TestScopeHeaderFitsNarrowWidths(t *testing.T) {
	m, now := scopeFixture(t, "tree")
	m.scope.tracked = true
	m.applyScope("a-very-long-project-name-to-filter", now)
	for _, previewWidth := range []int{0, 20} {
		for _, width := range []int{1, 10, 20, 24, 40, 80} {
			layout := layoutMetrics{listWidth: width, previewWidth: previewWidth}

			lines := strings.Split(m.viewHeader(layout), "\n")

			for _, line := range lines {
				require.LessOrEqual(t, terminal.Width(line), width)
			}
		}
	}
}
