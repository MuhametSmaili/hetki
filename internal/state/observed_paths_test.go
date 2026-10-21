package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComparePrefersKnownMatchesOverUnknownPaths(t *testing.T) {
	for _, tc := range []struct {
		name         string
		unknownPaths []string
		unknownLater bool
	}{
		{name: "unknown window directory", unknownPaths: []string{"", "/tools"}},
		{name: "unknown later pane directory", unknownPaths: []string{"/project", ""}},
		{name: "prefer known window directory between partial observations", unknownPaths: []string{"", "/tools"}, unknownLater: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := NewState()
			desired.AddSession("dev").Windows = []*Window{{
				Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/tools"}},
			}}
			known := &Window{ID: "@known", Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/tools"}}}
			if tc.unknownLater {
				known.Panes[1].Path = ""
			}
			actual := NewState()
			actual.AddSession("dev").Windows = []*Window{
				{ID: "@unknown", Name: "editor", Path: tc.unknownPaths[0], Panes: []*Pane{{Path: tc.unknownPaths[0]}, {Path: tc.unknownPaths[1]}}},
				known,
			}

			diff := Compare(desired, actual, nil).Windows["dev"]

			assert.Empty(t, diff.Missing)
			require.Len(t, diff.Extra, 1)
			assert.Equal(t, "@unknown", diff.Extra[0].ID)
			assert.Empty(t, diff.Mismatched)
		})
	}
}

func TestComparePreservesCompatibleDuplicatesWithUnknownPaths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reverse bool
	}{
		{name: "partial observation first"},
		{name: "known observation first", reverse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := NewState()
			desired.AddSession("dev").Windows = []*Window{
				{Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/a"}}},
				{Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/b"}}},
			}
			windows := []*Window{
				{ID: "@1", Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: ""}}},
				{ID: "@2", Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/b"}}},
			}
			if tc.reverse {
				windows[0], windows[1] = windows[1], windows[0]
			}
			actual := NewState()
			actual.AddSession("dev").Windows = windows

			diff := Compare(desired, actual, nil)

			assert.Empty(t, diff.Windows)
		})
	}
}

func TestCompareDoesNotPreferKnownWindowPathOverCompatiblePaneCount(t *testing.T) {
	desired := NewState()
	desired.AddSession("dev").Windows = []*Window{{
		Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/tools"}},
	}}
	actual := NewState()
	actual.AddSession("dev").Windows = []*Window{
		{ID: "@compatible", Name: "editor", Panes: []*Pane{{}, {Path: "/tools"}}},
		{ID: "@extra", Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {}, {Path: "/tools"}}},
	}

	diff := Compare(desired, actual, nil).Windows["dev"]

	assert.Empty(t, diff.Missing)
	assert.Empty(t, diff.Mismatched)
	require.Len(t, diff.Extra, 1)
	assert.Equal(t, "@extra", diff.Extra[0].ID)
}

func TestCompareKeepsObservationOrderForKnownMismatches(t *testing.T) {
	desired := NewState()
	desired.AddSession("dev").Windows = []*Window{{
		Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/tools"}},
	}}
	actual := NewState()
	actual.AddSession("dev").Windows = []*Window{
		{ID: "@first", Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/other"}}},
		{ID: "@second", Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project", Zoom: true}, {Path: "/tools"}}},
	}

	diff := Compare(desired, actual, nil).Windows["dev"]

	require.Len(t, diff.Mismatched, 1)
	assert.Equal(t, "@first", diff.Mismatched[0].Actual.ID)
	require.Len(t, diff.Extra, 1)
	assert.Equal(t, "@second", diff.Extra[0].ID)
	assert.Empty(t, diff.Missing)
}

func TestCompareConsumesUnknownCandidateOnlyOnce(t *testing.T) {
	desired := NewState()
	desired.AddSession("dev").Windows = []*Window{
		{Name: "editor", Path: "/first", Panes: []*Pane{{Path: "/first"}}},
		{Name: "editor", Path: "/second", Panes: []*Pane{{Path: "/second"}}},
	}
	actual := NewState()
	actual.AddSession("dev").Windows = []*Window{{ID: "@1", Name: "editor", Panes: []*Pane{{}}}}

	diff := Compare(desired, actual, nil).Windows["dev"]

	require.Len(t, diff.Missing, 1)
	assert.Equal(t, "/second", diff.Missing[0].Path)
	assert.Empty(t, diff.Mismatched)
	assert.Empty(t, diff.Extra)
}

func TestCompareDoesNotSkipEmptyDesiredPaths(t *testing.T) {
	for _, tc := range []struct {
		name           string
		paths          []string
		wantMissing    int
		wantMismatches int
	}{
		{name: "empty desired window path", paths: []string{"", "/tools"}, wantMissing: 1},
		{name: "empty desired pane path", paths: []string{"/project", ""}, wantMismatches: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := NewState()
			desired.AddSession("dev").Windows = []*Window{{
				Name: "editor", Path: tc.paths[0], Panes: []*Pane{{Path: tc.paths[0]}, {Path: tc.paths[1]}},
			}}
			actual := NewState()
			actual.AddSession("dev").Windows = []*Window{{
				Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/tools"}},
			}}

			diff := Compare(desired, actual, nil).Windows["dev"]

			assert.Len(t, diff.Missing, tc.wantMissing)
			assert.Len(t, diff.Extra, tc.wantMissing)
			assert.Len(t, diff.Mismatched, tc.wantMismatches)
		})
	}
}

func TestCompareRetainsLayoutPolicyWithUnknownPaths(t *testing.T) {
	for _, tc := range []struct {
		name           string
		layoutsMatch   func(string, string) bool
		wantMismatches int
	}{
		{name: "layout differs", wantMismatches: 1},
		{name: "backend recognizes layout", layoutsMatch: func(desired, actual string) bool {
			return desired == "tiled" && actual == "observed"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := NewState()
			desired.AddSession("dev").Windows = []*Window{{
				Name: "editor", Path: "/project", Layout: "tiled", Panes: []*Pane{{Path: "/project"}},
			}}
			actual := NewState()
			actual.AddSession("dev").Windows = []*Window{{Name: "editor", Layout: "observed", Panes: []*Pane{{}}}}

			diff := Compare(desired, actual, tc.layoutsMatch).Windows["dev"]

			assert.Empty(t, diff.Missing)
			assert.Empty(t, diff.Extra)
			assert.Len(t, diff.Mismatched, tc.wantMismatches)
		})
	}
}

func TestWindowsCorrespondStillRequiresExactPaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{name: "unknown window directory", paths: []string{"", "/tools"}},
		{name: "unknown later pane directory", paths: []string{"/project", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := &Window{Name: "editor", Path: "/project", Panes: []*Pane{{Path: "/project"}, {Path: "/tools"}}}
			actual := &Window{Name: "editor", Path: tc.paths[0], Panes: []*Pane{{Path: tc.paths[0]}, {Path: tc.paths[1]}}}

			correspond := WindowsCorrespond(desired, actual)

			assert.False(t, correspond, "save correspondence must not inherit permissive reconciliation")
		})
	}
}
