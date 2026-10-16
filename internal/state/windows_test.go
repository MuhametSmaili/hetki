package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareSessionWindowsDetectsSemanticMismatch(t *testing.T) {
	desired := []*Window{{
		Name:   "editor",
		Path:   "~/code",
		Layout: "shape-a",
		Panes: []*Pane{
			{Path: "~/code", Command: "vim"},
			{Path: "~/api", Command: "npm test", Zoom: true},
		},
	}}
	actual := []*Window{{
		Name:   "editor",
		Path:   "~/code",
		Layout: "shape-b",
		Panes: []*Pane{
			{Path: "~/code", Command: "vim"},
			{Path: "~/api", Command: "npm run dev"},
		},
	}}

	diff := (windowComparer{}).compareSessionWindows(desired, actual)
	require.Len(t, diff.Mismatched, 1)
	assert.Equal(t, "shape-a", diff.Mismatched[0].Desired.Layout)
	assert.Equal(t, "shape-b", diff.Mismatched[0].Actual.Layout)
	assert.Empty(t, diff.Missing)
	assert.Empty(t, diff.Extra)
}

func TestCompareSessionWindowsMatchesPanesByPosition(t *testing.T) {
	desired := []*Window{{
		Name: "editor",
		Path: "~/code",
		Panes: []*Pane{
			{Path: "~/code", Command: "vim"},
			{Path: "~/api", Command: "npm test"},
		},
	}}
	actual := []*Window{{
		Name: "editor",
		Path: "~/code",
		Panes: []*Pane{
			{Path: "~/api", Command: "npm test"},
			{Path: "~/code", Command: "vim"},
		},
	}}

	diff := (windowComparer{}).compareSessionWindows(desired, actual)
	require.Len(t, diff.Mismatched, 1)
	assert.Empty(t, diff.Missing)
	assert.Empty(t, diff.Extra)
}

func TestCompareSessionWindowsTreatsWindowPathChangeAsReplacement(t *testing.T) {
	desired := []*Window{{Name: "editor", Path: "~/new", Panes: []*Pane{{Path: "~/new"}}}}
	actual := []*Window{{Name: "editor", Path: "~/old", Panes: []*Pane{{Path: "~/old"}}}}

	diff := (windowComparer{}).compareSessionWindows(desired, actual)
	require.Len(t, diff.Missing, 1)
	require.Len(t, diff.Extra, 1)
	assert.Empty(t, diff.Mismatched)
	assert.Equal(t, "~/new", diff.Missing[0].Path)
	assert.Equal(t, "~/old", diff.Extra[0].Path)
}

func TestCompareSessionWindowsTreatsDuplicatesAsDeterministicMultiset(t *testing.T) {
	desired := []*Window{{Name: "editor", Path: "~/code", Panes: []*Pane{{Path: "~/code"}}}}
	actual := []*Window{
		{ID: "@1", Name: "editor", Path: "~/code", Panes: []*Pane{{Path: "~/different"}}},
		{ID: "@2", Name: "editor", Path: "~/scratch", Panes: []*Pane{{Path: "~/scratch"}}},
	}

	diff := (windowComparer{}).compareSessionWindows(desired, actual)
	require.Len(t, diff.Mismatched, 1)
	assert.Equal(t, "@1", diff.Mismatched[0].Actual.ID)
	require.Len(t, diff.Extra, 1)
	assert.Equal(t, "@2", diff.Extra[0].ID)
}

func TestCompareSessionWindowsPrefersExactDuplicateMatch(t *testing.T) {
	desired := []*Window{{Name: "editor", Path: "~/code", Panes: []*Pane{{Path: "~/code"}}}}
	actual := []*Window{
		{ID: "@1", Name: "editor", Path: "~/code", Panes: []*Pane{{Path: "~/different"}}},
		{ID: "@2", Name: "editor", Path: "~/code", Panes: []*Pane{{Path: "~/code"}}},
	}

	diff := (windowComparer{}).compareSessionWindows(desired, actual)
	assert.Empty(t, diff.Mismatched)
	require.Len(t, diff.Extra, 1)
	assert.Equal(t, "@1", diff.Extra[0].ID)
}

func TestWindowComparerUsesLayoutPolicyAndStrictPaneSemantics(t *testing.T) {
	comparer := windowComparer{layoutsMatch: func(desired, actual string) bool {
		return desired == "recipe"
	}}
	base := &Window{
		Name:   "editor",
		Path:   "~/code",
		Layout: "recipe",
		Panes: []*Pane{
			{Path: "~/code", Command: "vim"},
			{Path: "~/api", Command: "npm test", Zoom: true},
		},
	}

	assert.True(t, comparer.windowsMatch(base, &Window{
		Name:   "editor",
		Path:   "~/code",
		Layout: "recipe",
		Panes: []*Pane{
			{Path: "~/code", Command: "vim"},
			{Path: "~/api", Command: "npm test", Zoom: true},
		},
	}))
	assert.True(t, comparer.windowsMatch(&Window{Name: "editor", Path: "~/code", Layout: "recipe", Panes: base.Panes}, &Window{Name: "editor", Path: "~/code", Layout: "shape-a", Panes: base.Panes}))
	assert.False(t, comparer.windowsMatch(&Window{Name: "editor", Path: "~/code", Layout: "shape-a", Panes: base.Panes}, &Window{Name: "editor", Path: "~/code", Layout: "shape-b", Panes: base.Panes}))
	// D1: command is a creation-time effect and never part of steady-state
	// equality, so a drifted pane command must not flag the window.
	assert.True(t, comparer.windowsMatch(base, &Window{Name: "editor", Path: "~/code", Layout: "recipe", Panes: []*Pane{{Path: "~/code", Command: "vim"}, {Path: "~/api", Command: "npm run dev", Zoom: true}}}))
	assert.False(t, comparer.windowsMatch(base, &Window{Name: "editor", Path: "~/code", Layout: "recipe", Panes: []*Pane{{Path: "~/code", Command: "vim"}, {Path: "~/api", Command: "npm test"}}}), "zoom difference is still a mismatch")
}
