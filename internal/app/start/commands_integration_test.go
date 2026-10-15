//go:build integration

package start

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandsExecuteOnceAtCreationTime proves D1 on a real tmux server:
// manifest commands are typed into the created pane's shell and a second
// start neither re-sends them nor flags the pane as drifted.
func TestCommandsExecuteOnceAtCreationTime(t *testing.T) {
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is required for integration tests")
	}
	socketDir, err := os.MkdirTemp("/tmp", "hetki-start-cmds-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "tmux.sock")
	wrapperDir := t.TempDir()
	wrapper := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in attach-session|switch-client) exit 0;; esac\nexec %q -S %q \"$@\"\n", realTmux, socket)
	require.NoError(t, os.WriteFile(filepath.Join(wrapperDir, "tmux"), []byte(wrapper), 0755))
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	t.Cleanup(func() { _ = exec.Command(realTmux, "-S", socket, "kill-server").Run() })

	marker := filepath.Join(socketDir, "marker")
	b, err := backendtmux.NewBackend()
	require.NoError(t, err)

	workDir := t.TempDir()
	source := fmt.Sprintf(`sessions:
  - name: dev
    windows:
      - name: editor
        panes:
          - path: %s
            command: printf '%%s\n' 'hetki ; Enter C-c -foo' >> %s
`, workDir, marker)
	workspace, err := manifest.Parse([]byte(source), "dev.yaml", backendtmux.ValidateAuthoredLayout)
	require.NoError(t, err, "the loader pipeline is part of the behavior under test")

	service := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

	require.NoError(t, service.Run(context.Background(), Options{}))

	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(marker); err == nil {
			assert.Equal(t, "hetki ; Enter C-c -foo\n", string(data), "literal command text must run exactly once")
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command never ran in the pane")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Second start: no create actions are planned, so the command is not
	// re-sent (append marker must stay single-line).
	second, err := buildPlan(context.Background(), b, workspace, false)
	require.NoError(t, err)
	require.True(t, second.IsEmpty(), "second start must plan no actions, got %v", second.Actions)
	require.NoError(t, service.Run(context.Background(), Options{}))
	time.Sleep(1 * time.Second)
	if data, err := os.ReadFile(marker); err == nil {
		assert.Equal(t, "hetki ; Enter C-c -foo\n", string(data), "second start must not re-send commands")
	}
}
