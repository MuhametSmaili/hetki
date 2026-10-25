package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkspaceCompletionReportsInventoryErrors(t *testing.T) {
	for _, test := range []struct {
		name            string
		brokenDirectory bool
		wantOutput      string
	}{
		{"missing directory is quiet", false, ":4\n"},
		{"unexpected read error is reported", true, ":5\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home\n\x1b[31m")
			t.Setenv("HOME", home)
			debugPath := filepath.Join(t.TempDir(), "completion.log")
			t.Setenv("BASH_COMP_DEBUG_FILE", debugPath)
			if test.brokenDirectory {
				directory := filepath.Join(home, ".config", "hetki")
				require.NoError(t, os.MkdirAll(directory, 0700))
				writeWorkspaceFile(t, directory, "workspaces", "not a directory")
			}

			var output bytes.Buffer
			previous := rootCmd.OutOrStdout()
			rootCmd.SetOut(&output)
			t.Cleanup(func() { rootCmd.SetOut(previous) })

			err := executeWorkspaceCommand(t, "__complete", "start", "")

			require.NoError(t, err)
			require.Equal(t, test.wantOutput, output.String())
			if test.brokenDirectory {
				diagnostic, err := os.ReadFile(debugPath)
				require.NoError(t, err)
				require.Contains(t, string(diagnostic), "complete workspace names:")
				require.Contains(t, string(diagnostic), "home\\n")
				require.NotContains(t, string(diagnostic), "\x1b")
			} else {
				_, err := os.Stat(debugPath)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}

func TestUpdateVersionCompletionDoesNotSuggestFiles(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"empty tag", []string{"--version", ""}},
		{"partial tag", []string{"--version", "v0."}},
		{"equals form", []string{"--version=v0."}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			previous := rootCmd.OutOrStdout()
			rootCmd.SetOut(&output)
			t.Cleanup(func() { rootCmd.SetOut(previous) })

			err := executeWorkspaceCommand(t, append([]string{"__complete", "update"}, test.args...)...)

			require.NoError(t, err)
			require.Equal(t, ":4\n", output.String())
		})
	}
}
