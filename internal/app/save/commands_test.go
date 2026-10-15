package save

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestServiceRunPreservesAuthoredCommands(t *testing.T) {
	for _, test := range []struct {
		name          string
		windowCommand string
		paneCommands  []string
		want          []string
	}{
		{"single window command", "make dev", nil, []string{"make dev"}},
		{"single pane command", "", []string{"make dev"}, []string{"make dev"}},
		{"split window command", "make dev", []string{"", "make watch"}, []string{"make dev", "make watch"}},
		{"split pane commands", "", []string{"printf '%s' '$HOME; $(false)'", ""}, []string{"printf '%s' '$HOME; $(false)'", ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "workspace.yaml")
			authored := manifest.Window{Name: "editor", Path: directory, Command: test.windowCommand}
			for _, command := range test.paneCommands {
				authored.Panes = append(authored.Panes, manifest.Pane{Command: command})
			}
			writeCommandWorkspace(t, path, &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{authored}}}})
			window := backend.Window{Name: "editor", Path: directory, Layout: "tiled"}
			for range len(test.want) {
				window.Panes = append(window.Panes, backend.Pane{Path: directory, Command: "node", Program: "pi"})
			}
			stub := &stubBackend{queryResult: backend.StateResult{
				Sessions: []backend.Session{{Name: "dev", Windows: []backend.Window{window}}},
				Active:   backend.ActiveContext{Session: "dev"},
			}}
			service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

			_, err := service.Run(t.Context(), Options{Path: path})

			require.NoError(t, err)
			saved, err := manifest.NewFileLoader(path, acceptLayout).Load()
			require.NoError(t, err)
			panes := saved.Sessions[0].Windows[0].Panes
			require.Len(t, panes, len(test.want))
			for i, command := range test.want {
				require.Equal(t, command, panes[i].Command)
			}
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			_, err = service.Run(t.Context(), Options{Path: path})

			require.NoError(t, err)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "saving again must retain commands without rewriting their meaning")
		})
	}
}

func TestServiceRunPreservesCommandsAcrossScopeAndLiveChanges(t *testing.T) {
	for _, test := range []struct {
		name, otherSession, otherCommand string
		all                              bool
	}{
		{"current session", "retained", "echo retained", false},
		{"all sessions", "new", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			code, api := filepath.Join(home, "code"), filepath.Join(home, "api")
			require.NoError(t, os.Mkdir(code, 0700))
			require.NoError(t, os.Mkdir(api, 0700))
			path := filepath.Join(home, "workspace.yaml")
			writeCommandWorkspace(t, path, &manifest.Workspace{Sessions: []manifest.Session{
				{Name: "dev", Root: "~", Windows: []manifest.Window{
					{Name: "worker", Path: "code", Command: "make dev", Layout: "tiled", Panes: []manifest.Pane{{}, {Path: "../api", Command: "make watch"}}},
					{Name: "worker", Path: "code", Command: "echo single"},
					{Name: "discarded", Path: "code"},
				}},
				{Name: "retained", Root: "~", Windows: []manifest.Window{{Name: "other", Path: "api", Command: "echo retained"}}},
			}})
			stub := &stubBackend{queryResult: backend.StateResult{
				Active: backend.ActiveContext{Session: "dev"},
				Sessions: []backend.Session{
					{Name: "dev", Windows: []backend.Window{
						{Name: "worker", Path: code, Panes: []backend.Pane{{Path: code}}},
						{Name: "new", Path: api, Panes: []backend.Pane{{Path: api, Command: "node", Program: "pi"}}},
						{Name: "worker", Path: api, Layout: "even-vertical", Panes: []backend.Pane{{Path: code}, {Path: api, Zoom: true}}},
					}},
					{Name: "new", Windows: []backend.Window{{Name: "shell", Path: code, Panes: []backend.Pane{{Path: code, Command: "bash"}}}}},
				},
			}}
			service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

			_, err := service.Run(t.Context(), Options{Path: path, All: test.all})

			require.NoError(t, err)
			saved, err := manifest.NewFileLoader(path, acceptLayout).Load()
			require.NoError(t, err)
			require.Len(t, saved.Sessions, 2)
			windows := saved.Sessions[0].Windows
			require.Len(t, windows, 3)
			require.Equal(t, "echo single", windows[0].Panes[0].Command)
			require.Empty(t, windows[1].Panes[0].Command)
			require.Equal(t, "make dev", windows[2].Panes[0].Command)
			require.Equal(t, "make watch", windows[2].Panes[1].Command)
			require.Equal(t, "even-vertical", windows[2].Layout)
			require.True(t, windows[2].Panes[1].Zoom)
			require.Equal(t, test.otherSession, saved.Sessions[1].Name)
			require.Equal(t, test.otherCommand, saved.Sessions[1].Windows[0].Panes[0].Command)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(data), "path: ~/code")
		})
	}
}

func TestServiceRunRefusesUnsafeCommandPreservation(t *testing.T) {
	directory, other := t.TempDir(), t.TempDir()
	source := manifest.Window{Name: "worker", Path: directory, Panes: []manifest.Pane{
		{Path: directory, Command: "echo private-argument"}, {Path: other, Command: "make watch"},
	}}
	commandless := manifest.Window{Name: "worker", Path: directory, Panes: []manifest.Pane{{Path: directory}, {Path: other}}}
	live := backend.Window{Name: "worker", Path: directory, Panes: []backend.Pane{{Path: directory}, {Path: other}}}
	for _, test := range []struct {
		name    string
		saved   []manifest.Window
		live    []backend.Window
		matches string
		all     bool
	}{
		{"duplicate saved locations", []manifest.Window{source, commandless}, []backend.Window{live}, "2 saved matches and 1 live matches", false},
		{"duplicate live locations", []manifest.Window{source}, []backend.Window{live, live}, "1 saved matches and 2 live matches", false},
		{"removed window", []manifest.Window{source}, []backend.Window{{Name: "replacement", Path: directory, Panes: live.Panes}}, "1 saved matches and 0 live matches", false},
		{"removed pane", []manifest.Window{source}, []backend.Window{{Name: "worker", Path: directory, Panes: live.Panes[:1]}}, "1 saved matches and 0 live matches", false},
		{"added pane", []manifest.Window{source}, []backend.Window{{Name: "worker", Path: directory, Panes: []backend.Pane{{Path: directory}, {Path: other}, {Path: other}}}}, "1 saved matches and 0 live matches", false},
		{"reordered panes", []manifest.Window{source}, []backend.Window{{Name: "worker", Path: directory, Panes: []backend.Pane{{Path: other}, {Path: directory}}}}, "1 saved matches and 0 live matches", false},
		{"changed directory", []manifest.Window{source}, []backend.Window{{Name: "worker", Path: directory, Panes: []backend.Pane{{Path: directory}, {Path: directory}}}}, "1 saved matches and 0 live matches", false},
		{"duplicate live locations, all sessions", []manifest.Window{source}, []backend.Window{live, live}, "1 saved matches and 2 live matches", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace.yaml")
			writeCommandWorkspace(t, path, &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: test.saved}}})
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			stub := &stubBackend{queryResult: backend.StateResult{
				Active:   backend.ActiveContext{Session: "dev"},
				Sessions: []backend.Session{{Name: "dev", Windows: test.live}},
			}}
			service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

			_, err = service.Run(t.Context(), Options{Path: path, All: test.all})

			require.ErrorIs(t, err, ErrCommandPreservation)
			require.ErrorContains(t, err, "session \"dev\", window \"worker\"")
			require.ErrorContains(t, err, test.matches)
			require.NotContains(t, err.Error(), "private-argument")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
		})
	}
}

func writeCommandWorkspace(t *testing.T, path string, workspace *manifest.Workspace) {
	t.Helper()
	data, err := manifest.Marshal(workspace, path, acceptLayout)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
}
