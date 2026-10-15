package save

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceRunSavesBackendLayoutPreset(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "workspace.yaml")
	stub := &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{Name: "dev", Windows: []backend.Window{{
			Name: "editor", Path: directory, Layout: "opaque observation",
			Panes: []backend.Pane{{Path: directory}, {Path: directory}},
		}}}},
		Active: backend.ActiveContext{Session: "dev"},
	}}
	stub.presetHook = func(observed string) (string, error) {
		assert.Equal(t, "opaque observation", observed)
		return "tiled", nil
	}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

	_, err := service.Run(t.Context(), Options{Path: path})

	require.NoError(t, err)
	workspace, err := manifest.NewFileLoader(path, acceptLayout).Load()
	require.NoError(t, err)
	assert.Equal(t, "tiled", workspace.Sessions[0].Windows[0].Layout)
}

func TestServiceRunPreservesDestinationWhenBackendCannotExportLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	before := validSavedWorkspace()
	require.NoError(t, os.WriteFile(path, before, 0600))
	failure := errors.New("unsupported backend layout")
	stub := &stubBackend{presetErr: failure, queryResult: backend.StateResult{
		Sessions: []backend.Session{{Name: "dev", Windows: []backend.Window{{
			Name: "editor", Layout: "opaque observation",
			Panes: []backend.Pane{{Path: "/tmp"}, {Path: "/tmp"}},
		}}}},
	}}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

	_, err := service.Run(t.Context(), Options{Path: path, All: true})

	require.ErrorIs(t, err, failure)
	assert.ErrorContains(t, err, `session "dev": window "editor"`)
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
