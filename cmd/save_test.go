package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSaveRetainsAuthoredLayoutForAnotherSession(t *testing.T) {
	resetCommandGlobals()
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	layout := "cafe,80x24,0,0{40x24,0,0,0,39x24,41,0,1}"
	before := "sessions:\n  - name: retained\n    windows:\n      - name: editor\n        layout: " + layout + "\n"
	require.NoError(t, os.WriteFile(path, []byte(before), 0640))
	withStubBackend(t, &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{Name: "dev", Windows: []backend.Window{{Name: "editor", Path: t.TempDir()}}}},
		Active:   backend.ActiveContext{Session: "dev"},
	}})
	require.NoError(t, saveCmd.Flags().Set("file", path))

	err := runSave(saveCmd, nil)

	require.NoError(t, err)
	workspace, loadErr := manifest.NewFileLoader(path, tmux.ValidateAuthoredLayout).Load()
	require.NoError(t, loadErr)
	require.Len(t, workspace.Sessions, 2)
	require.Equal(t, "retained", workspace.Sessions[0].Name)
	require.Equal(t, layout, workspace.Sessions[0].Windows[0].Layout)
}

func TestSaveAllSessionsReplacesSavedSessionSet(t *testing.T) {
	for _, flag := range []string{"--all-sessions", "-a"} {
		t.Run(flag, func(t *testing.T) {
			directory := t.TempDir()
			path := writeWorkspaceFile(t, directory, "work.yaml", "sessions:\n  - name: offline\n    windows:\n      - name: editor\n")
			stub := &stubBackend{queryResult: backend.StateResult{Sessions: []backend.Session{
				{Name: "work", Windows: []backend.Window{{Name: "editor", Path: directory}}},
				{Name: "personal", Windows: []backend.Window{{Name: "shell", Path: directory}}},
			}}}
			withStubBackend(t, stub)

			captureLoggerOutput(t, func() {
				require.NoError(t, executeWorkspaceCommand(t, "save", "--file", path, flag))
			})

			workspace, err := manifest.NewFileLoader(path, tmux.ValidateAuthoredLayout).Load()
			require.NoError(t, err)
			require.Len(t, workspace.Sessions, 2)
			require.Equal(t, "work", workspace.Sessions[0].Name)
			require.Equal(t, "personal", workspace.Sessions[1].Name)
			require.Zero(t, stub.applyCalls)
			require.Zero(t, stub.attachCalls)
		})
	}
}

func TestRunSaveWritesCurrentSessionWorkspace(t *testing.T) {
	resetCommandGlobals()

	home := t.TempDir()
	t.Setenv("HOME", home)

	outputPath := filepath.Join(t.TempDir(), "workspace.yaml")
	stub := &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{
			Name: "dev",
			Windows: []backend.Window{{
				Name: "editor",
				Path: filepath.Join(home, "code", "hetki"),
			}},
		}},
		Active: backend.ActiveContext{Session: "dev"},
	}}
	withStubBackend(t, stub)
	require.NoError(t, saveCmd.Flags().Set("file", outputPath))

	logOutput := captureLoggerOutput(t, func() {
		require.NoError(t, runSave(saveCmd, nil))
	})

	assert.Contains(t, logOutput, "Saved to "+outputPath)
	assert.Zero(t, stub.applyCalls)

	loader := manifest.NewFileLoader(outputPath, tmux.ValidateAuthoredLayout)
	workspace, err := loader.Load()
	require.NoError(t, err)
	require.Len(t, workspace.Sessions, 1)
	assert.Equal(t, "dev", workspace.Sessions[0].Name)
	require.Len(t, workspace.Sessions[0].Windows, 1)
	assert.Equal(t, filepath.Join(home, "code", "hetki"), workspace.Sessions[0].Windows[0].Path)

	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "path: ~/code/hetki")
}
