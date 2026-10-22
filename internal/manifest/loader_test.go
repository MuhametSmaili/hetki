package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	yamlContent := `sessions:
  - name: myapp
    windows:
      - name: editor
        path: /home/user/code
        command: vim
      - name: server
        path: /home/user/code
        command: npm run dev
`

	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	loader := NewFileLoader(configPath, acceptLayout)
	workspace, err := loader.Load()
	require.NoError(t, err)

	assert.NotNil(t, workspace)
	assert.Len(t, workspace.Sessions, 1)
	assert.Equal(t, "myapp", workspace.Sessions[0].Name)
	assert.Len(t, workspace.Sessions[0].Windows, 2)
	assert.Equal(t, "editor", workspace.Sessions[0].Windows[0].Name)
	assert.Equal(t, "/home/user/code", workspace.Sessions[0].Windows[0].Path)
}

func TestLoadUnsupportedFormat(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.txt")

	err := os.WriteFile(configPath, []byte("invalid"), 0644)
	require.NoError(t, err)

	loader := NewFileLoader(configPath, acceptLayout)
	_, err = loader.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported config format")
}

func TestLoadFileNotFound(t *testing.T) {
	loader := NewFileLoader("/nonexistent/config.yaml", acceptLayout)
	_, err := loader.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read config")
}

func TestLoadYAMLWithPanes(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	yamlContent := `sessions:
  - name: myapp
    windows:
      - name: editor
        path: /home/user/code
        panes:
          - command: vim
          - command: npm run dev
`

	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	loader := NewFileLoader(configPath, acceptLayout)
	workspace, err := loader.Load()
	require.NoError(t, err)

	assert.NotNil(t, workspace)
	assert.Len(t, workspace.Sessions[0].Windows, 1)
	assert.Len(t, workspace.Sessions[0].Windows[0].Panes, 2)
	assert.Equal(t, "vim", workspace.Sessions[0].Windows[0].Panes[0].Command)
	assert.Equal(t, "/home/user/code", workspace.Sessions[0].Windows[0].Panes[1].Path, "panes inherit the window path")
}

// D2: split and size were accepted-but-ignored historically; they are now
// rejected with field paths.
func TestLoadYAMLRejectsUnsupportedPaneFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	yamlContent := `sessions:
  - name: myapp
    windows:
      - name: editor
        path: /home/user/code
        panes:
          - command: vim
            split: vertical
            size: 50
`

	err := os.WriteFile(configPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	_, err = NewFileLoader(configPath, acceptLayout).Load()
	require.Error(t, err)
	require.ErrorContains(t, err, "panes[0].split: unsupported field")
	assert.ErrorContains(t, err, "panes[0].size: unsupported field")
}

func TestLoadYAMLWithSessionRoot(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	root, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	yamlContent := fmt.Sprintf(`sessions:
  - name: myapp
    root: %q
    windows:
      - name: editor
      - name: server
        path: /other/path
`, root)

	err = os.WriteFile(configPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	loader := NewFileLoader(configPath, acceptLayout)
	workspace, err := loader.Load()
	require.NoError(t, err)

	assert.Equal(t, root, workspace.Sessions[0].Windows[0].Path)
	assert.Equal(t, "/other/path", workspace.Sessions[0].Windows[1].Path)
}

func TestScanWorkspaces(t *testing.T) {
	t.Run("scans directory with multiple files", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "project1.yaml"), []byte("sessions: []"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "project2.yml"), []byte("sessions: []"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "project3.json"), []byte(`{"sessions":[]}`), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("text"), 0644))

		paths, err := ScanWorkspaces(tmpDir)
		require.NoError(t, err)
		assert.Len(t, paths, 2)
		assert.Contains(t, paths, "project1")
		assert.Contains(t, paths, "project2")
		assert.NotContains(t, paths, "project3")
	})

	t.Run("returns empty for missing directory", func(t *testing.T) {
		paths, err := ScanWorkspaces("/nonexistent/directory")
		require.NoError(t, err)
		assert.Empty(t, paths)
	})

	t.Run("returns empty for empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		paths, err := ScanWorkspaces(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, paths)
	})

	t.Run("ignores subdirectories", func(t *testing.T) {
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "workspace.yaml"), []byte("sessions: []"), 0644))
		require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))

		paths, err := ScanWorkspaces(tmpDir)
		require.NoError(t, err)
		assert.Len(t, paths, 1)
		assert.Contains(t, paths, "workspace")
	})
}
