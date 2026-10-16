package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareWindows(t *testing.T) {
	desired := &State{Sessions: map[string]*Session{
		"s": {
			Name: "s",
			Windows: []*Window{
				{Name: "A", Path: "/A"},
				{Name: "B", Path: "/B"},
				{Name: "C", Path: "/C"},
			},
		},
	}}

	actual := &State{Sessions: map[string]*Session{
		"s": {
			Name: "s",
			Windows: []*Window{
				{Name: "A", Path: "/A-changed"}, // different path = missing + extra
				{Name: "C", Path: "/C"},         // match
				{Name: "D", Path: "/D"},         // extra
			},
		},
	}}

	diff := Compare(desired, actual, nil)

	// A|/A is missing (desired but not in actual)
	// B|/B is missing
	assert.Len(t, diff.Windows["s"].Missing, 2)

	// A|/A-changed is extra
	// D|/D is extra
	assert.Len(t, diff.Windows["s"].Extra, 2)

	// No mismatches since key includes path
	assert.Empty(t, diff.Windows["s"].Mismatched)
}

func TestCompareWindowsDuplicateNames(t *testing.T) {
	// tmux allows duplicate window names
	desired := &State{Sessions: map[string]*Session{
		"s": {
			Name: "s",
			Windows: []*Window{
				{Name: "editor", Path: "/project1"},
				{Name: "editor", Path: "/project2"},
			},
		},
	}}

	actual := &State{Sessions: map[string]*Session{
		"s": {
			Name: "s",
			Windows: []*Window{
				{Name: "editor", Path: "/project1"},
			},
		},
	}}

	diff := Compare(desired, actual, nil)

	// editor|/project2 is missing
	assert.Len(t, diff.Windows["s"].Missing, 1)
	assert.Equal(t, "/project2", diff.Windows["s"].Missing[0].Path)
}

func TestCompareWindowsMismatchedPaneCount(t *testing.T) {
	desired := &State{Sessions: map[string]*Session{
		"s": {
			Name: "s",
			Windows: []*Window{
				{
					Name:  "editor",
					Path:  "/home",
					Panes: []*Pane{{Path: "/a"}, {Path: "/b"}, {Path: "/c"}},
				},
			},
		},
	}}

	actual := &State{Sessions: map[string]*Session{
		"s": {
			Name: "s",
			Windows: []*Window{
				{
					Name:  "editor",
					Path:  "/home",
					Panes: []*Pane{{Path: "/x"}},
				},
			},
		},
	}}

	diff := Compare(desired, actual, nil)

	assert.Empty(t, diff.Windows["s"].Missing)
	assert.Empty(t, diff.Windows["s"].Extra)
	assert.Len(t, diff.Windows["s"].Mismatched, 1)
	assert.Equal(t, 3, len(diff.Windows["s"].Mismatched[0].Desired.Panes))
	assert.Equal(t, 1, len(diff.Windows["s"].Mismatched[0].Actual.Panes))
}

func TestCompareWindowsForMissingSession(t *testing.T) {
	desired := &State{Sessions: map[string]*Session{
		"new-session": {
			Name: "new-session",
			Windows: []*Window{
				{Name: "win1", Path: "/path1"},
				{Name: "win2", Path: "/path2"},
			},
		},
	}}

	actual := NewState()

	diff := Compare(desired, actual, nil)

	assert.Contains(t, diff.Sessions.Missing, "new-session")
	assert.Empty(t, diff.Windows["new-session"].Missing)
}
