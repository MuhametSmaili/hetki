package app

import (
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
