package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime/debug"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestApplyBuildInfoUsesModuleVersionAndRevision(t *testing.T) {
	oldVersion, oldCommit := Version, GitCommit
	t.Cleanup(func() { Version, GitCommit = oldVersion, oldCommit })
	Version, GitCommit = "dev", "unknown"

	applyBuildInfo(&debug.BuildInfo{
		Main:     debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}},
	})

	require.Equal(t, "v1.2.3", Version)
	require.Equal(t, "abc123", GitCommit)
}

func TestBareHetkiRequiresTerminalStdinAndStdout(t *testing.T) {
	previousProbe, previousOpen := isTerminal, openTUI
	t.Cleanup(func() {
		isTerminal, openTUI = previousProbe, previousOpen
	})

	for _, test := range []struct {
		name                string
		stdinTTY, stdoutTTY bool
		wantRuns            int
	}{
		{name: "neither", wantRuns: 0},
		{name: "stdin only", stdinTTY: true, wantRuns: 0},
		{name: "stdout only", stdoutTTY: true, wantRuns: 0},
		{name: "both", stdinTTY: true, stdoutTTY: true, wantRuns: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			isTerminal = func(fd uintptr) bool {
				switch fd {
				case os.Stdin.Fd():
					return test.stdinTTY
				case os.Stdout.Fd():
					return test.stdoutTTY
				default:
					t.Fatalf("unexpected terminal probe fd %d", fd)
					return false
				}
			}
			runs := 0
			openTUI = func(context.Context) error {
				runs++
				return nil
			}
			var output bytes.Buffer
			command := &cobra.Command{Use: "hetki", RunE: runBareHetki}
			command.SetOut(&output)

			require.NoError(t, runBareHetki(command, nil))
			require.Equal(t, test.wantRuns, runs)
			if test.wantRuns == 0 {
				require.Contains(t, output.String(), "Usage:")
			} else {
				require.Empty(t, output.String())
			}
		})
	}
}

func TestBareHetkiPreservesContextAndTUIError(t *testing.T) {
	previousProbe, previousOpen := isTerminal, openTUI
	t.Cleanup(func() {
		isTerminal, openTUI = previousProbe, previousOpen
	})
	isTerminal = func(uintptr) bool { return true }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	openTUI = func(got context.Context) error {
		require.ErrorIs(t, got.Err(), context.Canceled)
		return errors.New("tui failed after restore")
	}
	command := &cobra.Command{}
	command.SetContext(ctx)

	require.EqualError(t, runBareHetki(command, nil), "tui failed after restore")
}

func TestRootRejectsArgumentsAndRemovedCommands(t *testing.T) {
	withStubBackend(t, &stubBackend{})

	require.Error(t, rootCmd.Args(rootCmd, []string{"unexpected"}))
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	for _, removed := range []string{"tui", "list", "report", "switch"} {
		t.Run(removed, func(t *testing.T) {
			rootCmd.SetArgs([]string{removed, "dev"})

			_, err := rootCmd.ExecuteC()

			require.ErrorContains(t, err, `unknown command "`+removed+`"`)
		})
	}
}

func TestRootDoesNotExposePreviewConfiguration(t *testing.T) {
	require.Nil(t, rootCmd.Flags().Lookup("preview-width"))
	require.NotContains(t, rootCmd.Flags().FlagUsages(), "preview-width")
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	rootCmd.SetArgs([]string{"--preview-width", "60"})
	_, err := rootCmd.ExecuteC()
	require.ErrorContains(t, err, "unknown flag: --preview-width")
}

func TestUpdateRejectsRemovedFlags(t *testing.T) {
	require.NotNil(t, updateCmd.Flags().Lookup("head"))
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	for _, flag := range []string{"--source", "--pre"} {
		require.NotContains(t, updateCmd.Flags().FlagUsages(), flag)
		rootCmd.SetArgs([]string{"update", flag})
		_, err := rootCmd.ExecuteC()
		require.ErrorContains(t, err, "unknown flag: "+flag)
	}
}

func TestCommandSignalContextCancelsOnTerminationSignals(t *testing.T) {
	for _, terminationSignal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(terminationSignal.String(), func(t *testing.T) {
			ctx, stop := commandSignalContext()
			defer stop()
			process, err := os.FindProcess(os.Getpid())
			require.NoError(t, err)
			require.NoError(t, process.Signal(terminationSignal))

			select {
			case <-ctx.Done():
				require.ErrorIs(t, ctx.Err(), context.Canceled)
			case <-time.After(time.Second):
				t.Fatalf("command context did not observe %s", terminationSignal)
			}
		})
	}
}
