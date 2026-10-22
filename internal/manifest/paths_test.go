package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseResolvesDirectoriesThroughTheirParents(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "app", "backend", "tests"), 0700))
	t.Chdir(directory)
	source := `sessions:
  - name: dev
    root: app
    windows:
      - name: service
        path: backend
        command: run-server
        panes:
          - {}
          - path: tests
`

	workspace, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

	require.NoError(t, err)
	session := workspace.Sessions[0]
	assert.Equal(t, filepath.Join(directory, "app"), session.Root)
	window := session.Windows[0]
	assert.Equal(t, "service", window.Name)
	assert.Equal(t, filepath.Join(directory, "app", "backend"), window.Path)
	assert.Equal(t, []Pane{
		{Path: filepath.Join(directory, "app", "backend"), Command: "run-server"},
		{Path: filepath.Join(directory, "app", "backend", "tests")},
	}, window.Panes)
	assert.Empty(t, window.Command)
}

func TestParseDirectoryInheritanceCases(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	root := filepath.Join(directory, "app")
	backend := filepath.Join(root, "backend")
	tests := filepath.Join(backend, "tests")
	external := filepath.Join(directory, "external")
	require.NoError(t, os.MkdirAll(tests, 0700))
	require.NoError(t, os.MkdirAll(external, 0700))
	require.NoError(t, os.Symlink(root, filepath.Join(directory, "linked-root")))
	require.NoError(t, os.Symlink(backend, filepath.Join(root, "linked-backend")))
	t.Chdir(directory)
	t.Setenv("HOME", directory)
	t.Setenv("HETKI_ROOT", root)
	t.Setenv("HETKI_CHILD", "tests")

	for _, tc := range []struct {
		name      string
		root      string
		path      string
		panes     []Pane
		wantRoot  string
		wantPaths []string
	}{
		{name: "omitted paths inherit", root: "app", wantRoot: root, wantPaths: []string{root}},
		{name: "all directories omitted use caller directory", wantPaths: []string{directory}},
		{name: "rootless relative window uses caller directory", path: "app/backend", wantPaths: []string{backend}},
		{name: "rootless dot uses caller directory", path: ".", panes: []Pane{{Path: "."}}, wantPaths: []string{directory}},
		{name: "window parent can leave root", root: "app", path: "../external", wantRoot: root, wantPaths: []string{external}},
		{name: "pane dot and parent use window base", root: "app", path: "backend", panes: []Pane{{Path: "."}, {Path: ".."}, {Path: "../../external"}}, wantRoot: root, wantPaths: []string{backend, root, external}},
		{name: "absolute window overrides root", root: "app", path: external, wantRoot: root, wantPaths: []string{external}},
		{name: "absolute first pane does not rebase siblings", root: "app", path: "backend", panes: []Pane{{Path: external}, {}, {Path: "tests"}}, wantRoot: root, wantPaths: []string{external, backend, tests}},
		{name: "omitted window leaves panes relative to root", root: "app", panes: []Pane{{Path: "backend/tests"}, {}}, wantRoot: root, wantPaths: []string{tests, root}},
		{name: "rootless first pane leaves siblings relative to caller directory", panes: []Pane{{Path: "app/backend"}, {}}, wantPaths: []string{backend, directory}},
		{name: "home expansion", root: "~/app", path: "backend", panes: []Pane{{}, {Path: "~/external"}}, wantRoot: root, wantPaths: []string{backend, external}},
		{name: "environment expansion", root: "$HETKI_ROOT", path: "backend", panes: []Pane{{}, {Path: "$HETKI_CHILD"}}, wantRoot: root, wantPaths: []string{backend, tests}},
		{name: "symlinks use physical directories", root: "linked-root", path: "linked-backend", panes: []Pane{{}, {Path: "tests"}}, wantRoot: root, wantPaths: []string{backend, tests}},
		{name: "missing child retains unresolved components", root: "app", path: "missing/..", wantRoot: root, wantPaths: []string{root + "/missing/.."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := &Workspace{Sessions: []Session{{
				Name: "dev", Root: tc.root,
				Windows: []Window{{Name: "label", Path: tc.path, Panes: tc.panes}},
			}}}
			source, err := yaml.Marshal(raw)
			require.NoError(t, err)

			workspace, err := Parse(source, filepath.Join(external, "workspace.yaml"), acceptLayout)

			require.NoError(t, err)
			session := workspace.Sessions[0]
			assert.Equal(t, tc.wantRoot, session.Root)
			window := session.Windows[0]
			assert.Equal(t, "label", window.Name)
			assert.Equal(t, tc.wantPaths[0], window.Path)
			require.Len(t, window.Panes, len(tc.wantPaths))
			for i, path := range tc.wantPaths {
				assert.Equal(t, path, window.Panes[i].Path)
			}
		})
	}
}

func TestParseRootlessInheritanceUsesPhysicalWorkingDirectory(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(directory, alias))
	t.Chdir(alias)
	source := fmt.Sprintf("sessions:\n  - name: dev\n    windows:\n      - name: editor\n        panes:\n          - path: %q\n          - {}\n", t.TempDir())

	workspace, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

	require.NoError(t, err)
	assert.Equal(t, directory, workspace.Sessions[0].Windows[0].Panes[1].Path)
}

func TestParseDoesNotExpandWorkingDirectoryText(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cwd := filepath.Join(directory, "${HETKI_CWD_TEXT}")
	require.NoError(t, os.Mkdir(cwd, 0700))
	t.Chdir(cwd)
	t.Setenv("HETKI_CWD_TEXT", "wrong-directory")
	source := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: .\n"

	workspace, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

	require.NoError(t, err)
	assert.Equal(t, cwd, workspace.Sessions[0].Windows[0].Path)
}

func TestParseRequiresAnExistingDirectoryForRoot(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "file")
	require.NoError(t, os.WriteFile(file, []byte("not a directory"), 0600))
	brokenLink := filepath.Join(directory, "broken")
	require.NoError(t, os.Symlink(filepath.Join(directory, "missing"), brokenLink))
	t.Setenv("HETKI_EMPTY_ROOT", "")

	for _, tc := range []struct {
		name    string
		root    string
		message string
		cause   error
	}{
		{name: "missing", root: filepath.Join(directory, "missing"), cause: os.ErrNotExist},
		{name: "file", root: file, message: "not a directory"},
		{name: "broken symlink", root: brokenLink, cause: os.ErrNotExist},
		{name: "parent traversal through a missing directory", root: filepath.Join(directory, "missing") + "/..", cause: os.ErrNotExist},
		{name: "empty expansion", root: "$HETKI_EMPTY_ROOT", message: "expands to an empty value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("sessions:\n  - name: dev\n    root: %q\n    windows:\n      - name: editor\n", tc.root)

			_, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

			require.Error(t, err)
			require.ErrorContains(t, err, "sessions[0].root")
			if tc.cause != nil {
				require.ErrorIs(t, err, tc.cause)
			}
			if tc.message != "" {
				assert.ErrorContains(t, err, tc.message)
			}
		})
	}
}

func TestParseDoesNotGuessAnUnavailableHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	source := "sessions:\n  - name: dev\n    root: ~/tmp\n    windows:\n      - name: editor\n"

	_, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

	require.Error(t, err)
	require.ErrorContains(t, err, "sessions[0].root")
	assert.ErrorContains(t, err, "home directory")
}

func TestParseRejectsInvalidExpandedChildDirectories(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		pane    bool
		tilde   bool
		field   string
		message string
	}{
		{name: "empty window", field: "windows[0].path", message: "expands to an empty value"},
		{name: "empty pane", pane: true, field: "panes[0].path", message: "expands to an empty value"},
		{name: "newline in expanded pane", value: "bad\npath", pane: true, field: "panes[0].path", message: "contains a newline"},
		{name: "parent traversal cannot hide expanded control text", value: "bad\n/..", pane: true, field: "panes[0].path", message: "contains a newline"},
		{name: "tilde expansion cannot hide expanded control text", value: "bad\n/..", pane: true, tilde: true, field: "panes[0].path", message: "contains a newline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HETKI_BAD_PATH", tc.value)
			window := Window{Name: "editor", Path: "$HETKI_BAD_PATH"}
			if tc.pane {
				window.Path = t.TempDir()
				window.Panes = []Pane{{Path: "$HETKI_BAD_PATH"}}
			}
			if tc.tilde {
				t.Setenv("HOME", t.TempDir())
				window.Panes[0].Path = "~/$HETKI_BAD_PATH"
			}
			source, err := yaml.Marshal(&Workspace{Sessions: []Session{{Name: "dev", Windows: []Window{window}}}})
			require.NoError(t, err)

			_, err = Parse(source, "workspace.yaml", acceptLayout)

			require.Error(t, err)
			require.ErrorContains(t, err, tc.field)
			assert.ErrorContains(t, err, tc.message)
		})
	}
}

func TestParseRejectsInvalidHomeDirectoryText(t *testing.T) {
	t.Setenv("HOME", t.TempDir()+"/bad\n/..")
	source := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: ~/child\n"

	_, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

	require.Error(t, err)
	require.ErrorContains(t, err, "windows[0].path")
	assert.ErrorContains(t, err, "home directory path contains a newline")
}

func TestParseReportsUnresolvableDirectoryPaths(t *testing.T) {
	directory := t.TempDir()
	loop := filepath.Join(directory, "loop")
	require.NoError(t, os.Symlink(loop, loop))
	t.Chdir(directory)

	for _, path := range []string{"loop", "loop/.."} {
		t.Run(path, func(t *testing.T) {
			source := fmt.Sprintf("sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: %q\n", path)

			_, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

			require.Error(t, err)
			assert.ErrorContains(t, err, "sessions[0].windows[0].path")
		})
	}
}

func TestParsePreservesPhysicalParentTraversal(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo := filepath.Join(directory, "repo")
	external := filepath.Join(directory, "external")
	require.NoError(t, os.Mkdir(repo, 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(external, "child"), 0700))
	require.NoError(t, os.Symlink(filepath.Join(external, "child"), filepath.Join(repo, "link")))
	t.Chdir(repo)
	absolute := repo + "/link/.."

	for _, tc := range []struct {
		name     string
		root     string
		path     string
		pane     string
		wantRoot string
	}{
		{name: "relative root", root: "link/..", path: ".", wantRoot: external},
		{name: "absolute root", root: absolute, path: ".", wantRoot: external},
		{name: "relative window", root: repo, path: "link/..", wantRoot: repo},
		{name: "absolute window", root: repo, path: absolute, wantRoot: repo},
		{name: "relative pane", root: repo, path: ".", pane: "link/..", wantRoot: repo},
		{name: "absolute pane", root: repo, path: ".", pane: absolute, wantRoot: repo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := yaml.Marshal(&Workspace{Sessions: []Session{{
				Name: "dev", Root: tc.root,
				Windows: []Window{{Name: "editor", Path: tc.path, Panes: []Pane{{Path: tc.pane}}}},
			}}})
			require.NoError(t, err)

			workspace, err := Parse(source, "workspace.yaml", acceptLayout)

			require.NoError(t, err)
			session := workspace.Sessions[0]
			assert.Equal(t, tc.wantRoot, session.Root)
			assert.Equal(t, external, session.Windows[0].Path)
			assert.Equal(t, external, session.Windows[0].Panes[0].Path)
		})
	}
}

func TestMarshalPreservesAuthoredDirectoryBases(t *testing.T) {
	root := t.TempDir()
	raw := &Workspace{Sessions: []Session{{
		Name: "dev", Root: root,
		Windows: []Window{{Name: "editor", Path: "backend", Command: "run-server", Panes: []Pane{{Path: "tests"}, {}}}},
	}}}

	data, err := Marshal(raw, "workspace.yaml", acceptLayout)

	require.NoError(t, err)
	var authored Workspace
	require.NoError(t, yaml.Unmarshal(data, &authored))
	assert.Equal(t, raw, &authored)
	window := raw.Sessions[0].Windows[0]
	assert.Equal(t, "backend", window.Path)
	assert.Equal(t, "run-server", window.Command)
	assert.Equal(t, []Pane{{Path: "tests"}, {}}, window.Panes)
}

func TestParseFirstPaneOverrideKeepsSiblingBase(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	source := fmt.Sprintf(`sessions:
  - name: dev
    root: %q
    windows:
      - name: service
        path: backend
        panes:
          - path: tests
          - {}
          - path: sibling
`, directory)

	workspace, err := Parse([]byte(source), "workspace.yaml", acceptLayout)

	require.NoError(t, err)
	window := workspace.Sessions[0].Windows[0]
	assert.Equal(t, filepath.Join(directory, "backend", "tests"), window.Path)
	assert.Equal(t, []Pane{
		{Path: filepath.Join(directory, "backend", "tests")},
		{Path: filepath.Join(directory, "backend")},
		{Path: filepath.Join(directory, "backend", "sibling")},
	}, window.Panes)

	data, err := Marshal(workspace, "roundtrip.yaml", acceptLayout)
	require.NoError(t, err)
	restored, err := Parse(data, "roundtrip.yaml", acceptLayout)
	require.NoError(t, err)
	assert.Equal(t, workspace, restored, "serialized absolute pane paths must preserve the sibling base")
}
