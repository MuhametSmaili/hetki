package save

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func acceptLayout(string) error { return nil }

type stubBackend struct {
	queryResult backend.StateResult
	queryErr    error
	presetErr   error
	presetHook  func(string) (string, error)
}

func (s *stubBackend) LayoutPreset(observed string) (string, error) {
	if s.presetErr != nil {
		return "", s.presetErr
	}
	if s.presetHook != nil {
		return s.presetHook(observed)
	}
	return observed, nil
}

func (s *stubBackend) QueryState(context.Context) (backend.StateResult, error) {
	if s.queryErr != nil {
		return backend.StateResult{}, s.queryErr
	}
	return s.queryResult, nil
}

func TestServiceRunWritesCurrentSessionWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	outputPath := filepath.Join(t.TempDir(), "workspace.yaml")
	stub := &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{
			Name: "dev",
			Windows: []backend.Window{{
				Name:   "editor",
				Path:   filepath.Join(home, "code", "hetki"),
				Layout: "tiled",
				Panes:  []backend.Pane{{Path: filepath.Join(home, "code", "hetki"), Command: "node", Program: "pi"}},
			}},
		}},
		Active: backend.ActiveContext{Session: "dev"},
	}}

	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
	path, err := service.Run(context.Background(), Options{Path: outputPath})
	require.NoError(t, err)
	assert.Equal(t, outputPath, path)

	loader := manifest.NewFileLoader(outputPath, acceptLayout)
	workspace, err := loader.Load()
	require.NoError(t, err)
	require.Len(t, workspace.Sessions, 1)
	assert.Equal(t, "dev", workspace.Sessions[0].Name)
	require.Len(t, workspace.Sessions[0].Windows, 1)
	assert.Equal(t, filepath.Join(home, "code", "hetki"), workspace.Sessions[0].Windows[0].Path)
	assert.Empty(t, workspace.Sessions[0].Windows[0].Layout, "single-pane layouts remain omitted")
	assert.Empty(t, workspace.Sessions[0].Windows[0].Panes[0].Command, "foreground observations are not startup commands")

	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "path: ~/code/hetki")
}

func TestServiceRunPreservesDestinationOnLayoutRejection(t *testing.T) {
	denied := errors.New("unsupported authored layout")
	policy := func(text string) error {
		if text == "blocked" {
			return denied
		}
		return nil
	}

	for _, tc := range []struct {
		name, existingLayout, finalLayout string
		all                               bool
	}{
		{"replaced destination session", "blocked", "", false},
		{"all still validates destination", "blocked", "", true},
		{"final workspace", "", "blocked", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace.yaml")
			before := []byte("sessions:\n  - name: dev\n    windows:\n      - name: editor\n        layout: " + tc.existingLayout + "\n")
			require.NoError(t, os.WriteFile(path, before, 0640))
			directory := t.TempDir()
			stub := &stubBackend{queryResult: backend.StateResult{
				Sessions: []backend.Session{{Name: "dev", Windows: []backend.Window{{
					Name: "editor", Path: directory, Layout: tc.finalLayout,
					Panes: []backend.Pane{{Path: directory}, {Path: directory}},
				}}}},
				Active: backend.ActiveContext{Session: "dev"},
			}}
			service := NewService(func() (Backend, error) { return stub, nil }, policy)

			_, err := service.Run(t.Context(), Options{Path: path, All: tc.all})

			require.ErrorIs(t, err, denied)
			require.ErrorContains(t, err, "sessions[0].windows[0].layout")
			after, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, before, after)
			info, statErr := os.Stat(path)
			require.NoError(t, statErr)
			require.Equal(t, os.FileMode(0640), info.Mode().Perm())
		})
	}
}

func TestServiceRunRequiresLayoutPolicyBeforeDetection(t *testing.T) {
	service := NewService(func() (Backend, error) {
		t.Fatal("unexpected backend detection")
		return nil, nil
	}, nil)

	_, err := service.Run(t.Context(), Options{Path: filepath.Join(t.TempDir(), "workspace.yaml")})

	require.ErrorIs(t, err, manifest.ErrLayoutValidatorRequired)
}

func TestSaveWorkspacePreservesMalformedDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	before := []byte("sessions: [not valid")
	require.NoError(t, os.WriteFile(path, before, 0640))

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "existing destination is invalid")
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
}

func TestSaveWorkspaceRejectsOversizedDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = file.WriteAt([]byte{1}, manifest.MaxManifestBytes+1)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	_, err = saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "destination exceeds")
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Greater(t, info.Size(), int64(manifest.MaxManifestBytes), "oversized destination must remain untouched")
}

func TestSaveWorkspaceRejectsFinalSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	path := filepath.Join(dir, "workspace.yaml")
	before := validSavedWorkspace()
	require.NoError(t, os.WriteFile(target, before, 0644))
	require.NoError(t, os.Symlink(target, path))

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "must not be a symlink")
	after, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
	info, statErr := os.Lstat(path)
	require.NoError(t, statErr)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
}

func TestSaveWorkspaceRejectsWrongFormatAndDirectory(t *testing.T) {
	t.Run("removed JSON format", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "workspace.json")
		_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
		require.Error(t, err)
		assert.ErrorContains(t, err, "unsupported format")
		_, statErr := os.Lstat(path)
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("directory destination", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "workspace.yaml")
		require.NoError(t, os.Mkdir(path, 0755))
		_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not a regular file")
	})
}

func TestSaveWorkspacePreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	require.NoError(t, os.WriteFile(path, validSavedWorkspace(), 0600))

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSaveWorkspaceDetectsConflictBeforeCommit(t *testing.T) {
	resetSaveOperations(t)
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	before := validSavedWorkspace()
	concurrent := []byte("sessions:\n  - name: concurrent\n    windows:\n      - name: shell\n        path: /tmp\n")
	require.NoError(t, os.WriteFile(path, before, 0644))
	originalSync := syncSaveFile
	changed := false
	syncSaveFile = func(file *os.File) error {
		if !changed {
			changed = true
			require.NoError(t, os.WriteFile(path, concurrent, 0644))
		}
		return originalSync(file)
	}

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "changed since it was read")
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, concurrent, after, "the concurrent writer's bytes must not be replaced")
}

func TestSaveWorkspacePreservesPreviousBytesOnPreRenameFailure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		inject func()
	}{
		{name: "sync", inject: func() { syncSaveFile = func(*os.File) error { return errors.New("sync failed") } }},
		{name: "rename", inject: func() { renameSave = func(string, string) error { return errors.New("rename failed") } }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetSaveOperations(t)
			path := filepath.Join(t.TempDir(), "workspace.yaml")
			before := validSavedWorkspace()
			require.NoError(t, os.WriteFile(path, before, 0644))
			tt.inject()

			_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
			require.Error(t, err)
			after, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, before, after)
		})
	}
}

func TestSaveWorkspaceRollsBackWhenDirectorySyncFails(t *testing.T) {
	resetSaveOperations(t)
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	before := validSavedWorkspace()
	require.NoError(t, os.WriteFile(path, before, 0600))
	originalSyncDir := syncSaveDir
	calls := 0
	syncSaveDir = func(path string) error {
		calls++
		if calls == 1 {
			return errors.New("directory sync failed")
		}
		return originalSyncDir(path)
	}

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "replacement rolled back")
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSaveWorkspaceReportsUncertainStateWhenRollbackCannotBeSynced(t *testing.T) {
	resetSaveOperations(t)
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	before := validSavedWorkspace()
	require.NoError(t, os.WriteFile(path, before, 0644))
	syncSaveDir = func(string) error { return errors.New("directory sync failed") }

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "uncertain state")
}

func TestSaveWorkspaceRejectsUnreadableDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	before := validSavedWorkspace()
	require.NoError(t, os.WriteFile(path, before, 0600))
	require.NoError(t, os.Chmod(path, 0000))
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	if err == nil {
		t.Skip("filesystem privileges allow reading mode-000 files")
	}
	assert.ErrorContains(t, err, "open destination")
}

func TestSaveWorkspaceRollsBackNewFileWhenDirectorySyncFails(t *testing.T) {
	resetSaveOperations(t)
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	originalSyncDir := syncSaveDir
	calls := 0
	syncSaveDir = func(path string) error {
		calls++
		if calls == 1 {
			return errors.New("directory sync failed")
		}
		return originalSyncDir(path)
	}

	_, err := saveWorkspace(context.Background(), testSaveWorkspace(), path, false, acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "replacement rolled back")
	_, statErr := os.Lstat(path)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func resetSaveOperations(t *testing.T) {
	t.Helper()
	oldSyncFile, oldRename, oldRemove, oldSyncDir := syncSaveFile, renameSave, removeSave, syncSaveDir
	t.Cleanup(func() {
		syncSaveFile, renameSave, removeSave, syncSaveDir = oldSyncFile, oldRename, oldRemove, oldSyncDir
	})
}

func testSaveWorkspace() *manifest.Workspace {
	return &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{{Name: "editor", Path: "/work/hetki"}}}}}
}

func validSavedWorkspace() []byte {
	return []byte("sessions:\n  - name: old\n    windows:\n      - name: shell\n        path: /tmp\n")
}
