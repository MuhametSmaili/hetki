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

func TestUpdateRejectsUnsupportedArguments(t *testing.T) {
	require.NotNil(t, updateCmd.Flags().Lookup("head"))

	previousRun := updateCmd.RunE
	updateCmd.RunE = func(*cobra.Command, []string) error {
		return errors.New("update executed before argument validation")
	}
	t.Cleanup(func() {
		updateCmd.RunE = previousRun
		rootCmd.SetArgs(nil)
	})

	for _, test := range []struct {
		argument, want string
	}{
		{"--source", "unknown flag: --source"},
		{"--pre", "unknown flag: --pre"},
		{"unexpected", "unknown command"},
	} {
		t.Run(test.argument, func(t *testing.T) {
			rootCmd.SetArgs([]string{"update", test.argument})

			_, err := rootCmd.ExecuteC()

			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestVersionFlagsDoNotOpenPicker(t *testing.T) {
	previousOpen := openTUI
	openTUI = func(context.Context) error {
		t.Fatal("version request opened the picker")
		return nil
	}
	t.Cleanup(func() { openTUI = previousOpen })

	for _, flag := range []string{"--version", "-V", "-v"} {
		t.Run(flag, func(t *testing.T) {
			var output bytes.Buffer
			rootCmd.SetOut(&output)
			rootCmd.SetArgs([]string{flag})
			t.Cleanup(func() {
				rootCmd.SetOut(nil)
				rootCmd.SetArgs(nil)
				require.NoError(t, rootCmd.Flags().Set("version", "false"))
			})

			_, err := rootCmd.ExecuteC()

			if flag == "-v" {
				require.ErrorContains(t, err, "unknown shorthand flag: 'v'")
				require.Empty(t, output.String())
				return
			}
			require.NoError(t, err)
			require.Contains(t, output.String(), "hetki version "+rootCmd.Version)
			require.Contains(t, output.String(), "commit:")
			require.Contains(t, output.String(), "built:")
		})
	}
}

func TestUpdateFlagRouting(t *testing.T) {
	previousRun := updateCmd.RunE
	t.Cleanup(func() {
		updateCmd.RunE = previousRun
		rootCmd.SetArgs(nil)
	})

	for _, test := range []struct {
		name    string
		args    []string
		dryRun  bool
		verbose bool
		version string
	}{
		{name: "short dry-run", args: []string{"-n"}, dryRun: true},
		{name: "long dry-run", args: []string{"--dry-run"}, dryRun: true},
		{name: "short verbose", args: []string{"-v"}, verbose: true},
		{name: "long verbose", args: []string{"--verbose"}, verbose: true},
		{name: "exact release", args: []string{"--version", "v1.2.3", "-n", "-v"}, version: "v1.2.3", dryRun: true, verbose: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Cleanup(func() {
				require.NoError(t, updateCmd.Flags().Set("dry-run", "false"))
				require.NoError(t, updateCmd.Flags().Set("verbose", "false"))
				require.NoError(t, updateCmd.Flags().Set("version", ""))
			})
			called := false
			updateCmd.RunE = func(*cobra.Command, []string) error {
				called = true
				require.Equal(t, test.dryRun, updateDryRun)
				require.Equal(t, test.verbose, updateVerbose)
				require.Equal(t, test.version, updateTargetVer)
				return nil
			}
			rootCmd.SetArgs(append([]string{"update"}, test.args...))

			_, err := rootCmd.ExecuteC()

			require.NoError(t, err)
			require.True(t, called)
		})
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
