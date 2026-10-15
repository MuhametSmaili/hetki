package tui

import (
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestScopeHeaderKeepsLabelsFixedAndEmphasizesActiveScope(t *testing.T) {
	m, now := scopeFixture(t, "flat")
	m.mode = modeFilter
	all := strings.Split(m.viewHeader(80), "\n")[0]
	m.scope.tracked = true
	m.applyScope("idle", now)
	m.mode = modeJump

	tracked := strings.Split(m.viewHeader(80), "\n")[0]
	screen := uv.NewScreenBuffer(80, 1)
	uv.NewStyledString(tracked).Draw(screen, screen.Bounds())

	allText, trackedText := terminal.Sanitize(all), terminal.Sanitize(tracked)
	require.Contains(t, trackedText, "JUMP")
	require.Contains(t, trackedText, "idle")
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
}

func TestScopeHeaderFitsNarrowWidths(t *testing.T) {
	m, now := scopeFixture(t, "tree")
	m.scope.tracked = true
	m.applyScope("a-very-long-project-name-to-filter", now)
	widths := []int{1, 10, 24, 40, 80}
	lines := make([]string, len(widths))

	for i, width := range widths {
		lines[i] = strings.Split(m.viewHeader(width), "\n")[0]
	}

	for i, line := range lines {
		require.LessOrEqual(t, terminal.Width(line), widths[i])
	}
}
