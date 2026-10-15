package save

import (
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceRunPreservesPaneZoom(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "workspace.yaml")
	stub := &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{
			Name: "dev",
			Windows: []backend.Window{{
				Name: "editor", Path: directory,
				Panes: []backend.Pane{{Path: directory}, {Path: directory, Zoom: true}},
			}},
		}},
		Active: backend.ActiveContext{Session: "dev"},
	}}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

	_, err := service.Run(t.Context(), Options{Path: new(path)})

	require.NoError(t, err)
	workspace, err := manifest.NewFileLoader(path, acceptLayout).Load()
	require.NoError(t, err)
	require.Len(t, workspace.Sessions, 1)
	require.Len(t, workspace.Sessions[0].Windows, 1)
	panes := workspace.Sessions[0].Windows[0].Panes
	require.Len(t, panes, 2)
	assert.False(t, panes[0].Zoom)
	assert.True(t, panes[1].Zoom)
}
