package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeStrategyPlan(t *testing.T) {
	tests := []struct {
		name string
		diff Diff
		want []Action
	}{
		{
			name: "creates missing session",
			diff: Diff{
				Sessions: ItemDiff[Session]{
					Missing: []Session{{Name: "dev", Windows: []Window{{Name: "editor", Path: "~/code"}}}},
				},
				Windows: make(map[string]ItemDiff[Window]),
			},
			want: []Action{CreateSessionAction{Name: "dev", WindowName: "editor", Path: "~/code"}},
		},
		{
			name: "creates missing window",
			diff: Diff{
				Sessions: ItemDiff[Session]{},
				Windows: map[string]ItemDiff[Window]{
					"dev": {Missing: []Window{{Name: "server", Path: "~/api"}}},
				},
			},
			want: []Action{CreateWindowAction{Session: "dev", Name: "server", Path: "~/api"}},
		},
		{
			name: "ignores extra",
			diff: Diff{
				Sessions: ItemDiff[Session]{Extra: []Session{{Name: "old"}}},
				Windows:  map[string]ItemDiff[Window]{"dev": {Extra: []Window{{Name: "unused"}}}},
			},
			want: []Action{},
		},
		{
			name: "ignores mismatched",
			diff: Diff{
				Sessions: ItemDiff[Session]{},
				Windows: map[string]ItemDiff[Window]{
					"dev": {Mismatched: []Mismatch[Window]{{Desired: Window{Name: "editor"}, Actual: Window{Name: "editor"}}}},
				},
			},
			want: []Action{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := (&MergeStrategy{}).Plan(tt.diff)
			assert.Equal(t, tt.want, plan.Actions)
		})
	}
}

func TestForceStrategyPlan(t *testing.T) {
	tests := []struct {
		name string
		diff Diff
		want []Action
	}{
		{
			name: "preserves undeclared session",
			diff: Diff{
				Sessions: ItemDiff[Session]{Extra: []Session{{Name: "personal"}}},
				Windows:  make(map[string]ItemDiff[Window]),
			},
			want: []Action{},
		},
		{
			name: "kills extra window",
			diff: Diff{
				Sessions: ItemDiff[Session]{},
				Windows:  map[string]ItemDiff[Window]{"dev": {Extra: []Window{{ID: "@9", Name: "unused"}}}},
			},
			want: []Action{KillWindowAction{Session: "dev", Window: "unused", WindowID: "@9"}},
		},
		{
			name: "creates missing window before removing extra",
			diff: Diff{Windows: map[string]ItemDiff[Window]{
				"dev": {
					Missing: []Window{{Name: "editor", Path: "~/new"}},
					Extra:   []Window{{ID: "@3", Name: "legacy"}},
				},
			}},
			want: []Action{
				CreateWindowAction{Session: "dev", Name: "editor", Path: "~/new"},
				KillWindowAction{Session: "dev", Window: "legacy", WindowID: "@3"},
			},
		},
		{
			name: "finishes replacement follow-ups before removing old windows",
			diff: Diff{Windows: map[string]ItemDiff[Window]{
				"dev": {
					Extra: []Window{{ID: "@9", Name: "scratch"}},
					Mismatched: []Mismatch[Window]{{
						Actual: Window{ID: "@3", Name: "editor", Path: "~/code"},
						Desired: Window{
							Name:   "editor",
							Path:   "~/code",
							Layout: "tiled",
							Panes:  []Pane{{Path: "~/code"}, {Path: "~/api", Command: "npm test", Zoom: true}},
						},
					}},
				},
			}},
			want: []Action{
				CreateWindowAction{Session: "dev", Name: "editor", Path: "~/code"},
				SplitPaneAction{Session: "dev", Window: "editor", Path: "~/api"},
				SelectLayoutAction{Session: "dev", Window: "editor", Layout: "tiled"},
				SendKeysAction{Session: "dev", Window: "editor", Pane: 1, Command: "npm test"},
				ZoomPaneAction{Session: "dev", Window: "editor", Pane: 1},
				KillWindowAction{Session: "dev", Window: "editor", WindowID: "@3"},
				KillWindowAction{Session: "dev", Window: "scratch", WindowID: "@9"},
			},
		},
		{
			name: "recreates mismatched",
			diff: Diff{
				Sessions: ItemDiff[Session]{},
				Windows: map[string]ItemDiff[Window]{
					"dev": {Mismatched: []Mismatch[Window]{{Desired: Window{Name: "editor", Path: "~/new"}, Actual: Window{ID: "@3", Name: "editor", Path: "~/old"}}}},
				},
			},
			want: []Action{
				CreateWindowAction{Session: "dev", Name: "editor", Path: "~/new"},
				KillWindowAction{Session: "dev", Window: "editor", WindowID: "@3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := (&ForceStrategy{}).Plan(tt.diff)
			assert.Equal(t, tt.want, plan.Actions)
		})
	}
}

func TestPlanValidate(t *testing.T) {
	tests := []struct {
		name    string
		actions []Action
		wantErr bool
	}{
		{"valid session", []Action{CreateSessionAction{Name: "dev", Path: "~"}}, false},
		{"empty session name", []Action{CreateSessionAction{Name: "", Path: "~"}}, true},
		{"empty window name", []Action{CreateWindowAction{Session: "dev", Name: ""}}, true},
		{"empty window session", []Action{CreateWindowAction{Session: "", Name: "win"}}, true},
		{"stable window target", []Action{KillWindowAction{Session: "dev", Window: "editor", WindowID: "@1"}}, false},
		{"missing stable window target", []Action{KillWindowAction{Session: "dev", Window: "editor"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &Plan{Actions: tt.actions}
			err := plan.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStrategiesOrderWindowActionsBySessionName(t *testing.T) {
	diff := Diff{
		Windows: map[string]ItemDiff[Window]{
			"zeta":  {Missing: []Window{{Name: "z-window", Path: "~/z"}}},
			"alpha": {Missing: []Window{{Name: "a-window", Path: "~/a"}}},
		},
	}

	want := []Action{
		CreateWindowAction{Session: "alpha", Name: "a-window", Path: "~/a"},
		CreateWindowAction{Session: "zeta", Name: "z-window", Path: "~/z"},
	}

	for range 100 {
		plan := (&MergeStrategy{}).Plan(diff)
		assert.Equal(t, want, plan.Actions)
	}
}
