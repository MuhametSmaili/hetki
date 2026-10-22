package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestStrictDecodeRejectsUnknownFields(t *testing.T) {
	_, err := NewFileLoader(writeConfig(t, "w.yaml", "sessions:\n  - name: dev\n    windwos: []\n    windows:\n      - path: /p\n"), acceptLayout).Load()
	require.Error(t, err)
	assert.ErrorContains(t, err, "windwos")

}

func TestStrictDecodeRejectsDuplicateKeys(t *testing.T) {
	_, err := NewFileLoader(writeConfig(t, "w.yaml", "sessions:\n  - name: dev\n    name: ops\n    windows:\n      - path: /p\n"), acceptLayout).Load()
	require.Error(t, err, "yaml duplicate keys must fail")

}

func TestParseRejectsOversizedManifest(t *testing.T) {
	big := "sessions:\n  - name: dev\n    windows:\n      - path: /p\n" + strings.Repeat("#", MaxManifestBytes)
	_, err := Parse([]byte(big), "w.yaml", acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "exceeds")
}

func TestParseBoundsCardinalityAndLengths(t *testing.T) {
	tooMany := func(kind string, n int, line string) string {
		var b strings.Builder
		b.WriteString("sessions:\n  - name: dev\n    windows:\n      - path: /p\n")
		switch kind {
		case "sessions":
			b.Reset()
			b.WriteString("sessions:\n")
			for range n {
				b.WriteString("  - name: s\n    windows:\n      - path: /p\n")
			}
		case "windows":
			for range n {
				b.WriteString("      - path: /p\n")
			}
		case "panes":
			b.WriteString("        panes:\n")
			for range n {
				b.WriteString("          - path: /p\n")
			}
		}
		_ = line
		return b.String()
	}

	_, err := Parse([]byte(tooMany("sessions", MaxSessions+1, "")), "w.yaml", acceptLayout)
	require.Error(t, err)
	require.ErrorContains(t, err, "too many sessions")

	_, err = Parse([]byte(tooMany("windows", MaxWindowsPerSession+1, "")), "w.yaml", acceptLayout)
	require.Error(t, err)
	require.ErrorContains(t, err, "too many windows")

	_, err = Parse([]byte(tooMany("panes", MaxPanesPerWindow+1, "")), "w.yaml", acceptLayout)
	require.Error(t, err)
	require.ErrorContains(t, err, "too many panes")

	longName := "sessions:\n  - name: " + strings.Repeat("x", MaxNameLength+1) + "\n    windows:\n      - path: /p\n"
	_, err = Parse([]byte(longName), "w.yaml", acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "name exceeds")
}

func TestParseRejectsRemovedJSONFormat(t *testing.T) {
	_, err := Parse([]byte(`{"sessions":[]}`), "w.json", acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported config format: .json")
}

func TestParseRejectsWindowIndexField(t *testing.T) {
	_, err := NewFileLoader(writeConfig(t, "w.yaml", "sessions:\n  - name: dev\n    windows:\n      - path: /p\n        index: 3\n"), acceptLayout).Load()
	require.Error(t, err)
	assert.ErrorContains(t, err, "windows[0].index: unsupported field")
}

func TestParseCompilesFirstPaneAndCommandInheritance(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	source := fmt.Sprintf("sessions:\n  - name: dev\n    root: %q\n    windows:\n      - name: editor\n        command: vim\n      - name: server\n        path: /srv\n        panes:\n          - {}\n          - command: npm run dev\n", root)

	ws, err := Parse([]byte(source), "w.yaml", acceptLayout)
	require.NoError(t, err)

	editor := ws.Sessions[0].Windows[0]
	require.Len(t, editor.Panes, 1, "window without panes gets an explicit first pane")
	assert.Equal(t, root, editor.Panes[0].Path, "first pane inherits root-resolved window path")
	assert.Equal(t, "vim", editor.Panes[0].Command, "window command compiles into the first pane")

	server := ws.Sessions[0].Windows[1]
	require.Len(t, server.Panes, 2)
	assert.Equal(t, "/srv", server.Panes[0].Path, "implicit pane path inherits the window path")
	assert.Empty(t, server.Panes[0].Command)
	assert.Equal(t, "npm run dev", server.Panes[1].Command)
}

func TestParseRejectsInvalidNames(t *testing.T) {
	_, err := Parse([]byte("sessions:\n  - name: \"de\x01v\"\n    windows:\n      - path: /p\n"), "w.yaml", acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "control characters")
}

func TestScanWorkspacesPrecedenceForDuplicateBasenames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"dev.yml", "dev.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("sessions: []"), 0644))
	}

	paths, err := ScanWorkspaces(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "dev.yaml"), paths["dev"], ".yaml wins over .yml")
}

func TestMarshalRejectsOversizedOutput(t *testing.T) {
	panes := make([]Pane, MaxPanesPerWindow)
	for i := range panes {
		panes[i].Command = strings.Repeat("x", MaxCommandLength)
	}
	windows := make([]Window, 9)
	for i := range windows {
		windows[i] = Window{Name: "w", Path: "/p", Panes: panes}
	}

	_, err := Marshal(&Workspace{Sessions: []Session{{Name: "dev", Windows: windows}}}, "w.yaml", acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "marshaled manifest exceeds")
}

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	source := fmt.Sprintf("sessions:\n  - name: dev\n    root: %q\n    windows:\n      - name: editor\n        command: vim\n      - name: server\n        path: /srv\n        panes:\n          - command: npm run dev\n          - command: make watch\n            zoom: true\n", root)
	ws, err := Parse([]byte(source), "w.yaml", acceptLayout)
	require.NoError(t, err)

	data, err := Marshal(ws, "roundtrip.yaml", acceptLayout)
	require.NoError(t, err)
	ws2, err := Parse(data, "w.yaml", acceptLayout)
	require.NoError(t, err)
	assert.Equal(t, ws, ws2, "parse → marshal → parse must be stable")

	// Second parse of the same bytes must produce the same workspace
	// (idempotent normalization).
	ws3, err := Parse([]byte(source), "w.yaml", acceptLayout)
	require.NoError(t, err)
	assert.Equal(t, ws, ws3)
}

func TestStrictDecodeRejectsTrailingDocuments(t *testing.T) {
	doc := "sessions:\n  - name: dev\n    windows:\n      - path: /p\n"
	_, err := Parse([]byte(doc+"---\nsessions: []\n"), "w.yaml", acceptLayout)
	require.Error(t, err, "second yaml document must fail")
	assert.ErrorContains(t, err, "only one document")
}

func TestParseAllowsFirstPaneToOverrideWindowDirectory(t *testing.T) {
	workspace, err := Parse([]byte("sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: /a\n        panes:\n          - path: /b\n          - {}\n"), "w.yaml", acceptLayout)

	require.NoError(t, err)
	window := workspace.Sessions[0].Windows[0]
	assert.Equal(t, "/b", window.Path)
	assert.Equal(t, []Pane{{Path: "/b"}, {Path: "/a"}}, window.Panes)
}

func TestParseAdoptsFirstPanePathWhenWindowPathEmpty(t *testing.T) {
	source := fmt.Sprintf("sessions:\n  - name: dev\n    root: %q\n    windows:\n      - name: editor\n        panes:\n          - path: /tmp\n", t.TempDir())
	ws, err := Parse([]byte(source), "w.yaml", acceptLayout)
	require.NoError(t, err)
	editor := ws.Sessions[0].Windows[0]
	directory, err := filepath.EvalSymlinks("/tmp")
	require.NoError(t, err)
	assert.Equal(t, directory, editor.Path, "window start path adopts the first pane path")
	assert.Equal(t, directory, editor.Panes[0].Path)
	assert.Equal(t, "editor", editor.Name)
}

func TestParseRejectsConflictingWindowAndPaneCommand(t *testing.T) {
	_, err := Parse([]byte("sessions:\n  - name: dev\n    windows:\n      - path: /p\n        command: vim\n        panes:\n          - command: nvim\n"), "w.yaml", acceptLayout)
	require.Error(t, err)
	assert.ErrorContains(t, err, "window command conflicts with the first pane command")
}

func TestParseRejectsControlBytesInCommands(t *testing.T) {
	// YAML double-quoted scalars interpret \n and \t escapes.
	for _, cmd := range []string{`"echo a\nb"`, `"echo\ttab"`} {
		_, err := Parse([]byte("sessions:\n  - name: dev\n    windows:\n      - path: /p\n        panes:\n          - command: "+cmd+"\n"), "w.yaml", acceptLayout)
		require.Error(t, err, cmd)
	}

	_, err := Parse([]byte("sessions:\n  - name: dev\n    windows:\n      - path: /p\n        command: \";\"\n"), "w.yaml", acceptLayout)
	assert.NoError(t, err, "tmux escaping preserves a literal bare semicolon")
}

func TestParseValidatesNormalizedValues(t *testing.T) {
	t.Setenv("HETKI_LONG_PATH", "/"+strings.Repeat("x", MaxPathLength))
	_, err := Parse([]byte("sessions:\n  - name: dev\n    root: $HETKI_LONG_PATH\n    windows:\n      - name: editor\n"), "w.yaml", acceptLayout)
	require.Error(t, err)
	require.ErrorContains(t, err, "sessions[0].root: resolved path is too long")

	longBase := strings.Repeat("x", MaxNameLength+1)
	_, err = Parse([]byte("sessions:\n  - name: dev\n    windows:\n      - path: /tmp/"+longBase+"\n"), "w.yaml", acceptLayout)
	require.Error(t, err)
	require.ErrorContains(t, err, "windows[0].path")
	var pathError *os.PathError
	assert.ErrorAs(t, err, &pathError)
}

func TestParseResolvesExplicitPanePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws, err := Parse([]byte("sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: ~/one\n        panes:\n          - {}\n          - path: ~/two\n"), "w.yaml", acceptLayout)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "two"), ws.Sessions[0].Windows[0].Panes[1].Path)
}

func TestFileLoaderRejectsOversizedFileBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.yaml")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = f.WriteAt(make([]byte, 1), MaxManifestBytes+1) // > cap without allocating
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = NewFileLoader(path, acceptLayout).Load()
	require.Error(t, err)
	assert.ErrorContains(t, err, "exceeds")
}
