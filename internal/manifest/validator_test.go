package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name            string
		workspace       *Workspace
		wantErr         bool
		wantErrContains string
		wantErrCount    int
	}{
		{
			name: "valid workspace",
			workspace: &Workspace{
				Sessions: []Session{
					{
						Name: "dev",
						Windows: []Window{
							{Name: "editor", Path: "/home/user"},
							{Name: "terminal", Path: "/home/user"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:            "nil workspace",
			workspace:       nil,
			wantErr:         true,
			wantErrContains: "nil",
			wantErrCount:    1,
		},
		{
			name: "empty sessions",
			workspace: &Workspace{
				Sessions: []Session{},
			},
			wantErr:         true,
			wantErrContains: "at least one session is required",
			wantErrCount:    1,
		},
		{
			name: "empty session name",
			workspace: &Workspace{
				Sessions: []Session{
					{
						Name:    "",
						Windows: []Window{{Name: "editor", Path: "/home"}},
					},
				},
			},
			wantErr:         true,
			wantErrContains: "session name cannot be empty",
			wantErrCount:    1,
		},
		{
			name: "empty window list",
			workspace: &Workspace{
				Sessions: []Session{
					{Name: "dev", Windows: []Window{}},
				},
			},
			wantErr:         true,
			wantErrContains: "at least one window is required",
			wantErrCount:    1,
		},
		{
			name: "duplicate window names allowed",
			workspace: &Workspace{
				Sessions: []Session{
					{
						Name: "dev",
						Windows: []Window{
							{Name: "editor", Path: "/home"},
							{Name: "editor", Path: "/home"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate session names",
			workspace: &Workspace{
				Sessions: []Session{
					{Name: "dev", Windows: []Window{{Name: "a", Path: "/home"}}},
					{Name: "dev", Windows: []Window{{Name: "b", Path: "/home"}}},
				},
			},
			wantErr:         true,
			wantErrContains: "duplicate session name",
			wantErrCount:    1,
		},
		{
			name: "windows without names",
			workspace: &Workspace{
				Sessions: []Session{
					{
						Name: "dev",
						Windows: []Window{
							{Path: "/home/user"},
							{Path: "/home/other"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "multiple zoomed panes in one window",
			workspace: &Workspace{
				Sessions: []Session{
					{
						Name: "dev",
						Windows: []Window{
							{
								Name: "editor",
								Path: "/home/user",
								Panes: []Pane{
									{Path: "/home/user", Zoom: true},
									{Path: "/home/user", Zoom: true},
								},
							},
						},
					},
				},
			},
			wantErr:         true,
			wantErrContains: "zoom=true",
			wantErrCount:    1,
		},
		{
			name: "single zoomed pane",
			workspace: &Workspace{
				Sessions: []Session{
					{
						Name: "dev",
						Windows: []Window{
							{
								Name: "editor",
								Path: "/home/user",
								Panes: []Pane{
									{Path: "/home/user", Zoom: true},
									{Path: "/home/user"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := Validate(tt.workspace, acceptLayout)

			if tt.wantErr {
				assert.Len(t, errs, tt.wantErrCount)
				assert.ErrorContains(t, ToError(errs), tt.wantErrContains)
			} else {
				assert.Empty(t, errs, "expected no validation errors, got: %v", errs)
			}
		})
	}
}
