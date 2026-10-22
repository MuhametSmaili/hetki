package tmux

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadStateQuery(t *testing.T) {
	q := LoadStateQuery{}

	t.Run("args", func(t *testing.T) {
		expected := []string{
			"start-server",
			";", "show-options", "-gv", "base-index",
			";", "show-options", "-gv", "pane-base-index",
			";", "list-panes", "-a", "-F",
			"#{session_id}|#{q:session_name}|#{window_id}|#{q:window_name}|#{window_index}|#{window_layout}|#{window_zoomed_flag}|#{window_active}|#{pane_id}|#{pane_index}|#{pane_active}|#{q:pane_current_path}|#{q:pane_current_command}|#{q:" + backend.WorkspacePathOption + "}",
		}
		assert.Equal(t, expected, q.Args())
	})

	t.Setenv("TMUX", "")

	tests := []struct {
		name   string
		output string
		want   LoadStateResult
	}{
		{
			name:   "single session single window single pane",
			output: "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|",
			want: LoadStateResult{
				Sessions: []Session{{
					ID:   "$1",
					Name: "dev",
					Windows: []Window{{
						ID:     "@1",
						Name:   "editor",
						Index:  0,
						Path:   "~/code",
						Layout: "layout-a",
						Active: true,
						Panes:  []Pane{{ID: "%1", Index: 0, Path: "~/code", Command: "vim", Active: true}},
					}},
				}},
			},
		},
		{
			name:   "multiple panes same window",
			output: "0\n1\n$1|dev|@1|editor|0|layout-a|1|1|%1|0|0|~/code|vim|\n$1|dev|@1|editor|0|layout-a|1|1|%2|1|1|~/api|node|",
			want: LoadStateResult{
				Sessions: []Session{{
					ID:   "$1",
					Name: "dev",
					Windows: []Window{{
						ID:     "@1",
						Name:   "editor",
						Index:  0,
						Path:   "~/code",
						Layout: "layout-a",
						Active: true,
						Panes:  []Pane{{ID: "%1", Index: 0, Path: "~/code", Command: "vim"}, {ID: "%2", Index: 1, Path: "~/api", Command: "node", Zoom: true, Active: true}},
					}},
				}},
				PaneBaseIndex: 1,
			},
		},
		{
			name:   "multiple windows",
			output: "1\n1\n$1|dev|@1|editor|0|layout-a|0|0|%1|0|0|~/code|vim|\n$1|dev|@2|server|1|layout-b|0|1|%2|0|1|~/api|node|",
			want: LoadStateResult{
				Sessions: []Session{{
					ID:   "$1",
					Name: "dev",
					Windows: []Window{
						{ID: "@1", Name: "editor", Index: 0, Path: "~/code", Layout: "layout-a", Panes: []Pane{{ID: "%1", Index: 0, Path: "~/code", Command: "vim"}}},
						{ID: "@2", Name: "server", Index: 1, Path: "~/api", Layout: "layout-b", Active: true, Panes: []Pane{{ID: "%2", Index: 0, Path: "~/api", Command: "node", Active: true}}},
					},
				}},
				WindowBaseIndex: 1,
				PaneBaseIndex:   1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := q.Parse(tt.output)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadStateQueryScopesActivePaneToActiveWindow(t *testing.T) {
	q := LoadStateQuery{}
	t.Setenv("TMUX", ",,1")
	output := "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/active|vim|\n$1|dev|@2|logs|1|layout-b|0|0|%2|0|1|~/inactive|tail|"

	got, err := q.Parse(output)
	require.NoError(t, err)
	assert.Equal(t, ActiveContext{SessionID: "$1", Session: "dev", WindowID: "@1", Window: "editor", WindowIndex: 0, PaneID: "%1", Pane: 0, Path: "~/active"}, got.Active)
}

func TestLoadStateQueryRejectsMalformedIndexesAndIDs(t *testing.T) {
	q := LoadStateQuery{}
	for _, output := range []string{
		"",
		"bad\n0",
		"-1\n0",
		"0\n-1",
		"0\n0\n$1|dev|@1|editor|bad|layout|0|1|%1|0|1|~/code|vim|",
		"0\n0\n$bad|dev|@1|editor|0|layout|0|1|%1|0|1|~/code|vim|",
		"0\n0\n$1|dev|@bad|editor|0|layout|0|1|%1|0|1|~/code|vim|",
		"0\n0\n$1|dev|@1|editor|0|layout|0|1|%bad|0|1|~/code|vim|",
	} {
		_, err := q.Parse(output)
		assert.Error(t, err, output)
	}
}

func TestLoadStateQueryParsesWorkspacePathOption(t *testing.T) {
	q := LoadStateQuery{}
	t.Setenv("TMUX", "")

	output := "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|/tmp/workspace.yaml"

	got, err := q.Parse(output)
	require.NoError(t, err)
	if assert.Len(t, got.Sessions, 1) {
		assert.Equal(t, "/tmp/workspace.yaml", got.Sessions[0].WorkspacePath)
	}
}

func TestLoadStateQueryOrdersSessionsAndWindows(t *testing.T) {
	q := LoadStateQuery{}
	t.Setenv("TMUX", "")

	output := "0\n0\n$2|zeta|@4|server|1|layout-z1|0|1|%4|0|1|~/zeta/server|node|\n$2|zeta|@3|editor|0|layout-z0|0|0|%3|0|0|~/zeta/editor|vim|\n$1|alpha|@2|worker|1|layout-a1|0|1|%2|0|1|~/alpha/worker|make|\n$1|alpha|@1|editor|0|layout-a0|0|0|%1|0|0|~/alpha/editor|vim|"

	want := []Session{
		{
			ID:   "$1",
			Name: "alpha",
			Windows: []Window{
				{ID: "@1", Name: "editor", Index: 0, Path: "~/alpha/editor", Layout: "layout-a0", Panes: []Pane{{ID: "%1", Index: 0, Path: "~/alpha/editor", Command: "vim"}}},
				{ID: "@2", Name: "worker", Index: 1, Path: "~/alpha/worker", Layout: "layout-a1", Active: true, Panes: []Pane{{ID: "%2", Index: 0, Path: "~/alpha/worker", Command: "make", Active: true}}},
			},
		},
		{
			ID:   "$2",
			Name: "zeta",
			Windows: []Window{
				{ID: "@3", Name: "editor", Index: 0, Path: "~/zeta/editor", Layout: "layout-z0", Panes: []Pane{{ID: "%3", Index: 0, Path: "~/zeta/editor", Command: "vim"}}},
				{ID: "@4", Name: "server", Index: 1, Path: "~/zeta/server", Layout: "layout-z1", Active: true, Panes: []Pane{{ID: "%4", Index: 0, Path: "~/zeta/server", Command: "node", Active: true}}},
			},
		},
	}

	for range 100 {
		got, err := q.Parse(output)
		require.NoError(t, err)
		assert.Equal(t, want, got.Sessions)
	}
}

// qEscape mirrors the escaping tmux applies to #{q:...} values for the
// characters relevant to row parsing: backslash and the '|' separator must be
// escaped; the rest of tmux's metacharacter set is escaped here too so the
// property test exercises unescaping.
func qEscape(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `|`, `\|`, `'`, `\'`, `"`, `\"`, `$`, `\$`, ` `, `\ `, `#`, `\#`, `(`, `\(`, `)`, `\)`, `;`, `\;`, `*`, `\*`, `<`, `\<`, `>`, `\>`, `?`, `\?`, `[`, `\[`, `&`, `\&`, "`", "\\`")
	return replacer.Replace(s)
}

func paneRow(sessionName, windowName, path, command, workspace string) string {
	return strings.Join([]string{
		"$1", qEscape(sessionName), "@1", qEscape(windowName), "0", "layout-a", "0", "1", "%1", "0", "1", qEscape(path), qEscape(command), qEscape(workspace),
	}, "|")
}

func TestLoadStateQueryRoundTripsEscapedValues(t *testing.T) {
	q := LoadStateQuery{}
	t.Setenv("TMUX", "")

	values := []string{
		`plain`,
		`with|pipe`,
		`back\slash`,
		`single'quote`,
		`double"quote`,
		`dollar$sign`,
		`space name`,
		`mixed |\ ' " $ # ( ) ; * < > ? [ & tab-adjacent`,
		`日本語|🎉→`,
		`trailing space `,
		` leading space`,
		`backslash-at-end\`,
		`semi;colon*glob?quest[bracket&amp`,
	}
	for _, name := range values {
		output := "0\n0\n" + paneRow(name, name, "/tmp/"+name, "vim"+name, "/tmp/ws|"+name)

		got, err := q.Parse(output)
		if assert.NoError(t, err, name) && assert.Len(t, got.Sessions, 1, name) {
			assert.Equal(t, name, got.Sessions[0].Name, name)
			assert.Equal(t, name, got.Sessions[0].Windows[0].Name, name)
			assert.Equal(t, "/tmp/"+name, got.Sessions[0].Windows[0].Panes[0].Path, name)
			assert.Equal(t, "vim"+name, got.Sessions[0].Windows[0].Panes[0].Command, name)
			assert.Equal(t, "/tmp/ws|"+name, got.Sessions[0].WorkspacePath, name)
		}
	}
}

func TestLoadStateQueryPropertyRoundTrip(t *testing.T) {
	q := LoadStateQuery{}
	t.Setenv("TMUX", "")

	rng := rand.New(rand.NewSource(1))
	alphabet := []rune(`abcXYZ019 \'"$#()[]{}*<>?;&|~^日本語🎉` + "\t\x7f")
	values := make([]string, 0, 200)
	for range 200 {
		runes := make([]rune, rng.Intn(24))
		for i := range runes {
			runes[i] = alphabet[rng.Intn(len(alphabet))]
		}
		values = append(values, string(runes))
	}

	for _, name := range values {
		output := "0\n0\n" + paneRow(name, name, name, name, name)
		got, err := q.Parse(output)
		require.NoError(t, err, name)
		if assert.Len(t, got.Sessions, 1, name) {
			assert.Equal(t, name, got.Sessions[0].Name, name)
		}
	}
}

func TestLoadStateQueryRejectsUnrepresentableRows(t *testing.T) {
	q := LoadStateQuery{}
	t.Setenv("TMUX", "")

	cases := map[string]string{
		"newline in value":         "0\n0\n$1|de\nv|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|",
		"newline in last field":    "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|/tmp/w\ns|",
		"value ends with newline":  "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|x\n\n",
		"value is only newline":    "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|\n\n",
		"blank interior line":      "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|\n\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|",
		"two trailing blank lines": "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|\n\n\n",
		"missing workspace field":  "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim",
		"extra field":              "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim||x|",
		"dangling escape":          `0` + "\n" + `0` + "\n" + `$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim\`,
	}
	for name, output := range cases {
		_, err := q.Parse(output)
		assert.Error(t, err, name)
	}
}

func TestLoadStateQueryAcceptsRecordTerminator(t *testing.T) {
	q := LoadStateQuery{}
	t.Setenv("TMUX", "")

	// Real tmux terminates every record with '\n'; the structural trailing
	// newline must be accepted even though value newlines are rejected.
	output := "0\n0\n$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|\n"

	got, err := q.Parse(output)
	require.NoError(t, err)
	if assert.Len(t, got.Sessions, 1) {
		assert.Empty(t, got.Sessions[0].WorkspacePath)
	}
}

func FuzzParsePaneLine(f *testing.F) {
	f.Add(`$1|dev|@1|editor|0|layout-a|0|1|%1|0|1|~/code|vim|`)
	f.Add(`$1|with\|pipe|@1|win\\|0|l|0|1|%1|0|1|/tmp/a\ b|v\'im|/ws|`)
	f.Add(`$1|日本語|@1|🎉|0|l|0|1|%1|0|1|/tmp|sh|`)
	f.Add(``)
	f.Add(`\\\|\|`)
	f.Fuzz(func(t *testing.T, line string) {
		// The invariant is that arbitrary input never panics; a value is
		// returned only when the row is exactly representable.
		_, _ = parsePaneLine(line)
	})
}
