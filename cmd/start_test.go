package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunStartValidatesManifestBeforeBackendDetection(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	previous := detectBackend
	detectBackend = func(...string) (backend.Backend, error) {
		t.Fatal("invalid manifest reached backend detection")
		return nil, nil
	}
	t.Cleanup(func() { detectBackend = previous })

	for _, tc := range []struct {
		name  string
		yaml  string
		flags []string
		want  []string
	}{
		{"invalid layout", "sessions:\n  - name: dev\n    windows:\n      - layout: diagonal\n", nil, []string{"sessions[0].windows[0].layout", "unknown layout"}},
		{"invalid detached workspace", "sessions: []\n", []string{"-d"}, []string{"at least one session is required"}},
		{"empty pruning workspace", "sessions: []\n", []string{"--prune-sessions"}, []string{"at least one session is required"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWorkspaceFile(t, t.TempDir(), "workspace.yaml", tc.yaml)
			args := append([]string{"start", "--file", path}, tc.flags...)

			err := executeWorkspaceCommand(t, args...)

			for _, message := range tc.want {
				require.ErrorContains(t, err, message)
			}
		})
	}
}

func TestStartAttachmentFlags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		flags      []string
		wantApply  int
		wantAttach int
		wantDryRun int
	}{
		{name: "default attaches", wantApply: 2, wantAttach: 1},
		{name: "long detach", flags: []string{"--detach"}, wantApply: 2},
		{name: "short detach executes instead of dry-run", flags: []string{"-d"}, wantApply: 2},
		{name: "explicit attach", flags: []string{"--detach=false"}, wantApply: 2, wantAttach: 1},
		{name: "long dry-run", flags: []string{"--dry-run"}, wantDryRun: 1},
		{name: "short dry-run", flags: []string{"-n"}, wantDryRun: 1},
		{name: "combined short flags", flags: []string{"-dn"}, wantDryRun: 1},
		{name: "dry-run with long detach", flags: []string{"--detach", "--dry-run"}, wantDryRun: 1},
		{name: "dry-run with short detach", flags: []string{"--dry-run", "-d"}, wantDryRun: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWorkspaceFile(t, t.TempDir(), "work.yaml", "sessions:\n  - name: dev\n    windows:\n      - name: editor\n")
			stub := &stubBackend{}
			withStubBackend(t, stub)
			args := append([]string{"start", "--file", path}, tc.flags...)

			err := executeWorkspaceCommand(t, args...)

			require.NoError(t, err)
			assert.Equal(t, tc.wantApply, stub.applyCalls)
			assert.Equal(t, tc.wantAttach, stub.attachCalls)
			assert.Equal(t, tc.wantDryRun, stub.dryRunCalls)
		})
	}
}

func TestStartHelpDocumentsCurrentUsageAndSafety(t *testing.T) {
	var output bytes.Buffer
	previous := rootCmd.OutOrStdout()
	rootCmd.SetOut(&output)
	t.Cleanup(func() {
		rootCmd.SetOut(previous)
		require.NoError(t, startCmd.Flags().Set("help", "false"))
	})

	err := executeWorkspaceCommand(t, "start", "--help")

	require.NoError(t, err)
	assert.Contains(t, output.String(), "-d, --detach")
	assert.Contains(t, output.String(), "-n, --dry-run")
	assert.Contains(t, output.String(), "-f, --file FILE")
	assert.NotContains(t, output.String(), "-f, --force")
	assert.Contains(t, output.String(), "Replace mismatched windows and delete extra windows in listed sessions")
	assert.Contains(t, output.String(), "Delete sessions not listed in the workspace")
	assert.Contains(t, output.String(), "start (NAME | --file FILE)")
	assert.Contains(t, output.String(), "Create missing sessions and windows with their configured panes.")
	assert.Contains(t, output.String(), "Leave existing windows unchanged unless --force is used.")
	assert.NotContains(t, output.String(), "Migration:")
	assert.NotContains(t, output.String(), "--name")
	assert.NotContains(t, output.String(), "--path")
	assert.Contains(t, output.String(), "including unrelated sessions")
	assert.Contains(t, output.String(), "earlier changes are not rolled back")
	assert.Contains(t, output.String(), "Use --dry-run first")
}

func TestRunStartDryRunOutputsPlan(t *testing.T) {
	resetCommandGlobals()

	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + tmpDir + "\n"
	writeWorkspaceFile(t, tmpDir, ".hetki.yaml", workspace)

	t.Chdir(tmpDir)

	stub := &stubBackend{dryRunLines: []string{"tmux new-session -d -s dev -n editor -c " + tmpDir}}
	withStubBackend(t, stub)
	dryRun = true
	require.NoError(t, startCmd.Flags().Set("file", "./.hetki.yaml"))

	output := captureLoggerOutput(t, func() {
		require.NoError(t, runStart(startCmd, nil))
	})

	assert.Contains(t, output, "Dry run - actions to execute:")
	assert.Contains(t, output, stub.dryRunLines[0])
	assert.Equal(t, 1, stub.dryRunCalls)
	assert.Zero(t, stub.applyCalls)
	assert.Zero(t, stub.attachCalls)
	if assert.Len(t, stub.lastActions, 1) {
		assert.IsType(t, backend.CreateSessionAction{}, stub.lastActions[0])
	}
}

func TestRunStartFailsWhenBackendStateQueryFails(t *testing.T) {
	resetCommandGlobals()

	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	writeWorkspaceFile(t, tmpDir, ".hetki.yaml", workspace)

	t.Chdir(tmpDir)

	stub := &stubBackend{queryErr: errors.New("query failed")}
	withStubBackend(t, stub)

	require.NoError(t, startCmd.Flags().Set("file", "./.hetki.yaml"))

	err := runStart(startCmd, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to query backend state: query failed")
	assert.Zero(t, stub.dryRunCalls)
	assert.Zero(t, stub.applyCalls)
}
