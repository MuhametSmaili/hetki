package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceLoaderPartialDefaultsRetainLayoutPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	require.NoError(t, os.WriteFile(path, []byte("sessions:\n  - name: dev\n    windows:\n      - layout: opaque\n"), 0600))
	denied := errors.New("unsupported layout")
	loader := NewWorkspaceLoader(func(string) error { return denied })
	loader.Resolve = func(string) (string, error) { return path, nil }
	loader.LoadFile = nil

	_, _, err := loader.LoadWorkspace("dev")

	require.ErrorIs(t, err, denied)
	assert.ErrorContains(t, err, "sessions[0].windows[0].layout")
}

func TestWorkspaceLoaderLoadWorkspace(t *testing.T) {
	loader := WorkspaceLoader{
		validateLayout: func(string) error { return nil },
		Resolve: func(nameOrPath string) (string, error) {
			assert.Equal(t, "dev", nameOrPath)
			return "/tmp/dev.yaml", nil
		},
		LoadFile: func(path string) (*manifest.Workspace, error) {
			assert.Equal(t, "/tmp/dev.yaml", path)
			return &manifest.Workspace{
				Sessions: []manifest.Session{{
					Name: "dev",
					Windows: []manifest.Window{{
						Name: "editor",
						Path: "/tmp/editor",
					}},
				}},
			}, nil
		},
	}

	workspace, path, err := loader.LoadWorkspace("dev")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/dev.yaml", path)
	require.Len(t, workspace.Sessions, 1)
	assert.Equal(t, "dev", workspace.Sessions[0].Name)
}

func TestWorkspaceLoaderLoadWorkspaceWrapsLoaderError(t *testing.T) {
	loader := WorkspaceLoader{
		validateLayout: func(string) error { return nil },
		Resolve:        func(string) (string, error) { return "/tmp/dev.yaml", nil },
		LoadFile: func(string) (*manifest.Workspace, error) {
			return nil, errors.New("boom")
		},
	}

	_, _, err := loader.LoadWorkspace("dev")
	require.Error(t, err)
	assert.ErrorContains(t, err, "loading workspace: boom")
}

func TestWorkspaceFromSessionsContractsHomePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace, err := WorkspaceFromSessions([]backend.Session{{
		Name: "dev",
		Windows: []backend.Window{{
			Name:  "editor",
			Path:  filepath.Join(home, "code", "hetki"),
			Panes: []backend.Pane{{Path: filepath.Join(home, "code", "hetki")}, {Path: filepath.Join(home, "api")}},
		}},
	}}, func(observed string) (string, error) { return observed, nil })

	require.NoError(t, err)
	require.Len(t, workspace.Sessions, 1)
	require.Len(t, workspace.Sessions[0].Windows, 1)
	assert.Equal(t, "~/code/hetki", workspace.Sessions[0].Windows[0].Path)
	assert.Empty(t, workspace.Sessions[0].Windows[0].Layout, "missing observations do not invent a preset")
	require.Len(t, workspace.Sessions[0].Windows[0].Panes, 2)
	assert.Equal(t, "~/api", workspace.Sessions[0].Windows[0].Panes[1].Path)
}

func TestMergeWorkspacesReplacesAndAppendsSessions(t *testing.T) {
	existing := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev"}, {Name: "ops"}}}
	incoming := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{{Name: "editor", Path: "/tmp/editor"}}}, {Name: "new"}}}

	merged := MergeWorkspaces(existing, incoming)
	require.Len(t, merged.Sessions, 3)
	assert.Equal(t, "dev", merged.Sessions[0].Name)
	require.Len(t, merged.Sessions[0].Windows, 1)
	assert.Equal(t, "new", merged.Sessions[2].Name)
}
