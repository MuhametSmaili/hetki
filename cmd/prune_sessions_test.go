package cmd

import (
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/require"
)

func TestPruningFlagBelongsOnlyToStart(t *testing.T) {
	err := executeWorkspaceCommand(t, "save", "work", "--prune-sessions")

	require.ErrorContains(t, err, "unknown flag: --prune-sessions")
}

func TestStartPruningIsIndependentOfForce(t *testing.T) {
	for _, tc := range []struct {
		name         string
		fileFlag     string
		flags        []string
		killSessions int
		killWindows  int
	}{
		{name: "additive", fileFlag: "--file"},
		{name: "short file is additive", fileFlag: "-f"},
		{name: "force only", fileFlag: "--file", flags: []string{"--force"}, killWindows: 1},
		{name: "prune only", fileFlag: "--file", flags: []string{"--prune-sessions"}, killSessions: 1},
		{name: "force and prune", fileFlag: "-f", flags: []string{"--force", "--prune-sessions"}, killSessions: 1, killWindows: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			path := writeWorkspaceFile(t, directory, "workspace.yaml", "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: "+directory+"\n")
			stub := &stubBackend{queryResult: backend.StateResult{Sessions: []backend.Session{
				{ID: "$1", Name: "dev", Windows: []backend.Window{
					{ID: "@1", Name: "editor", Path: directory, Panes: []backend.Pane{{ID: "%1", Path: directory}}},
					{ID: "@2", Name: "scratch", Path: directory, Panes: []backend.Pane{{ID: "%2", Path: directory}}},
				}},
				{ID: "$2", Name: "old", Windows: []backend.Window{{ID: "@3", Name: "shell", Path: directory, Panes: []backend.Pane{{ID: "%3", Path: directory}}}}},
			}}}
			withStubBackend(t, stub)
			args := append([]string{"start", tc.fileFlag, path, "--dry-run"}, tc.flags...)

			captureLoggerOutput(t, func() {
				require.NoError(t, executeWorkspaceCommand(t, args...))
			})

			require.Zero(t, stub.applyCalls)
			require.Zero(t, stub.attachCalls)
			var sessions, windows int
			for _, action := range stub.lastActions {
				switch action := action.(type) {
				case backend.KillSessionAction:
					sessions++
					require.Equal(t, "$2", action.Name)
				case backend.KillWindowAction:
					windows++
					require.Equal(t, "@2", action.WindowID)
				default:
					t.Fatalf("unexpected action: %T", action)
				}
			}
			require.Equal(t, tc.killSessions, sessions)
			require.Equal(t, tc.killWindows, windows)
			if tc.killSessions != 0 {
				require.IsType(t, backend.KillSessionAction{}, stub.lastActions[len(stub.lastActions)-1], "session pruning must be last")
			}
		})
	}
}
