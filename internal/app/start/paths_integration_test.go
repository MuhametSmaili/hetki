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

	"github.com/MuhametSmaili/hetki/internal/backend"
	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRelativeDirectoriesStartIdempotentlyOnIsolatedTmux(t *testing.T) {
	for _, tc := range []struct {
		name      string
		root      string
		panes     []string
		wantPaths []string
	}{
		{name: "rootless relative window", panes: []string{""}, wantPaths: []string{"backend"}},
		{name: "relative root and first-pane override", root: ".", panes: []string{"tests", "", ".."}, wantPaths: []string{"backend/tests", "backend", "."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newIsolatedStartBackend(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()

			directory, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(filepath.Join(directory, "backend", "tests"), 0700))
			configDirectory := filepath.Join(directory, "configs")
			require.NoError(t, os.Mkdir(configDirectory, 0700))
			t.Chdir(directory)
			require.NoError(t, b.Apply(ctx, []backend.Action{
				backend.CreateSessionAction{Name: "personal", WindowName: "shell", Path: directory},
			}))
			before, err := b.QueryState(ctx)
			require.NoError(t, err)

			marker := filepath.Join(directory, "startup")
			panes := make([]manifest.Pane, len(tc.panes))
			for i, path := range tc.panes {
				panes[i].Path = path
			}
			panes[0].Command = fmt.Sprintf("printf 'started\\n' >> %q; tmux wait-for -S directory-started", marker)
			raw := &manifest.Workspace{Sessions: []manifest.Session{{
				Name: "dev", Root: tc.root,
				Windows: []manifest.Window{{Name: "service", Path: "backend", Layout: "tiled", Panes: panes}},
			}}}
			data, err := yaml.Marshal(raw)
			require.NoError(t, err)
			workspacePath := filepath.Join(configDirectory, "workspace.yaml")
			require.NoError(t, os.WriteFile(workspacePath, data, 0600))
			service := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)

			err = service.Run(ctx, Options{Workspace: workspacePath})

			require.NoError(t, err)
			output, err := exec.CommandContext(ctx, "tmux", "wait-for", "directory-started").CombinedOutput()
			require.NoError(t, err, "startup command did not finish: %s", output)
			created, err := b.QueryState(ctx)
			require.NoError(t, err)
			window := findBackendWindow(t, findBackendSession(t, created.Sessions, "dev").Windows, "service")
			require.Len(t, window.Panes, len(tc.wantPaths))
			for i, path := range tc.wantPaths {
				assert.Equal(t, filepath.Join(directory, path), window.Panes[i].Path)
			}
			assert.Equal(t, window.Panes[0].Path, window.Path)

			for _, force := range []bool{false, true} {
				workspace, err := manifest.NewFileLoader(workspacePath, backendtmux.ValidateAuthoredLayout).Load()
				require.NoError(t, err)
				second, err := buildPlan(ctx, b, workspace, force)
				require.NoError(t, err)
				require.True(t, second.IsEmpty(), "reloaded second start must plan no actions (force=%v): %v", force, second.Actions)

				require.NoError(t, service.Run(ctx, Options{Workspace: workspacePath, Force: force}))
			}

			after, err := b.QueryState(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"dev", "personal"}, backendSessionNames(after.Sessions))
			assert.Equal(t, findBackendSession(t, before.Sessions, "personal"), findBackendSession(t, after.Sessions, "personal"))
			dev := findBackendSession(t, after.Sessions, "dev")
			assert.Equal(t, findBackendSession(t, created.Sessions, "dev").ID, dev.ID)
			require.Len(t, dev.Windows, 1)
			assert.Equal(t, window.ID, dev.Windows[0].ID)
			for i, pane := range dev.Windows[0].Panes {
				assert.Equal(t, window.Panes[i].ID, pane.ID)
			}
			content, err := os.ReadFile(marker)
			require.NoError(t, err)
			assert.Equal(t, "started\n", string(content), "matching starts must not repeat authored startup commands")
		})
	}
}
