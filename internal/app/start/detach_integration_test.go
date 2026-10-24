//go:build integration

package start

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDetachedStartPreservesClientSelectionAndPaneOrder(t *testing.T) {
	server := newDetachedStartServer(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	directories := []string{
		filepath.Join(root, "editor"),
		filepath.Join(root, "tools"),
		filepath.Join(root, "tests"),
	}
	panes := make([]manifest.Pane, len(directories))
	for i, directory := range directories {
		require.NoError(t, os.MkdirAll(directory, 0700))
		panes[i] = manifest.Pane{
			Path:    directory,
			Command: fmt.Sprintf("printf ready > command-ran; %q -S %q wait-for -S startup-ready-%d", server.bin, server.socket, i),
		}
	}

	server.run(t, "new-session", "-d", "-s", "dev", "-n", "existing", "-c", root, "/bin/sh")
	server.attach(t, "dev")
	selection := server.run(t, "list-clients", "-F", "#{client_name}|#{session_id}|#{window_id}|#{pane_id}")
	sessionID := server.run(t, "display-message", "-p", "-t", "dev", "#{session_id}")
	t.Setenv("TMUX", server.socket+",0,"+strings.TrimPrefix(sessionID, "$"))
	t.Setenv("TMUX_PANE", server.run(t, "display-message", "-p", "-t", "dev", "#{pane_id}"))

	workspace := manifest.Workspace{Sessions: []manifest.Session{
		{Name: "dev", Root: root, Windows: []manifest.Window{
			{Name: "existing"},
			{Name: "editor", Layout: "tiled", Panes: panes},
		}},
		{Name: "ops", Root: root, Windows: []manifest.Window{{Name: "logs"}}},
	}}
	data, err := yaml.Marshal(workspace)
	require.NoError(t, err)
	path := filepath.Join(root, "work.yaml")
	require.NoError(t, os.WriteFile(path, data, 0600))

	b, err := backendtmux.NewBackend()
	require.NoError(t, err)
	service := NewService(func() (Backend, error) { return b, nil }, backendtmux.ValidateAuthoredLayout)

	err = service.Run(server.ctx, Options{Path: new(path), Detach: true})

	require.NoError(t, err)
	for i, directory := range directories {
		server.run(t, "wait-for", fmt.Sprintf("startup-ready-%d", i))
		contents, err := os.ReadFile(filepath.Join(directory, "command-ran"))
		require.NoError(t, err)
		assert.Equal(t, "ready", string(contents))
	}

	assert.Equal(t, selection, server.run(t, "list-clients", "-F", "#{client_name}|#{session_id}|#{window_id}|#{pane_id}"))
	assert.Equal(t, strings.Join(directories, "\n"), server.run(t, "list-panes", "-t", "dev:editor", "-F", "#{pane_current_path}"))
	assert.Equal(t, "1\n0\n0", server.run(t, "list-panes", "-t", "dev:editor", "-F", "#{pane_active}"), "detached splitting must leave the first pane selected")

	output := captureLoggerOutput(t, func() {
		err = service.Run(server.ctx, Options{Path: new(path), Detach: true, DryRun: true, Force: true})
	})

	require.NoError(t, err)
	assert.Contains(t, output, "Workspace already up to date", "a second force preview must not replace the created windows")
	_, err = os.Stat(server.navigationLog)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

type detachedStartServer struct {
	ctx           context.Context
	bin           string
	socket        string
	navigationLog string
}

func newDetachedStartServer(t *testing.T) detachedStartServer {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is required for integration tests")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)

	directory := testenv.SocketDir(t)
	server := detachedStartServer{
		ctx:           ctx,
		bin:           bin,
		socket:        filepath.Join(directory, "tmux.sock"),
		navigationLog: filepath.Join(directory, "navigation.log"),
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, bin, "-S", server.socket, "kill-server").Run()
	})

	wrapperDir := t.TempDir()
	wrapper := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in attach-session|switch-client) printf '%%s\\n' \"$*\" >> %q; exit 1;; esac\nexec %q -f /dev/null -S %q \"$@\"\n", server.navigationLog, bin, server.socket)
	require.NoError(t, os.WriteFile(filepath.Join(wrapperDir, "tmux"), []byte(wrapper), 0755))

	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("SHELL", "/bin/sh")
	return server
}

func (s detachedStartServer) run(t *testing.T, args ...string) string {
	t.Helper()
	args = append([]string{"-f", "/dev/null", "-S", s.socket}, args...)
	output, err := exec.CommandContext(s.ctx, s.bin, args...).CombinedOutput()
	require.NoError(t, err, "%s", output)
	return strings.TrimSpace(string(output))
}

func (s detachedStartServer) attach(t *testing.T, session string) {
	t.Helper()
	cmd := exec.CommandContext(s.ctx, s.bin, "-S", s.socket, "-C", "attach-session", "-t", session)
	input, err := cmd.StdinPipe()
	require.NoError(t, err)

	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = input.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	_, err = fmt.Fprintln(input, "wait-for -S client-ready")
	require.NoError(t, err)
	s.run(t, "wait-for", "client-ready")
}
