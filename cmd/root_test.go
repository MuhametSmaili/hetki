package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"testing"
	"time"

	apptui "github.com/MuhametSmaili/hetki/internal/app/tui"
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
			openTUI = func(context.Context, apptui.Overrides) error {
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

func TestBareHetkiLoadsConfigOnlyForTerminal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("XDG_STATE_HOME", home)
	path := filepath.Join(home, "hetki", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("ui: {view: invalid}"), 0600))
	backendFailure := errors.New("backend reached despite invalid config")
	withStubBackend(t, &stubBackend{queryErr: backendFailure})
	previousProbe := isTerminal
	t.Cleanup(func() { isTerminal = previousProbe })

	for _, tty := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonterminal", true: "terminal"}[tty], func(t *testing.T) {
			isTerminal = func(uintptr) bool { return tty }
			var output bytes.Buffer
			command := &cobra.Command{Use: "hetki", RunE: runBareHetki}
			command.SetContext(t.Context())
			command.SetOut(&output)

			err := runBareHetki(command, nil)

			if tty {
				require.ErrorIs(t, err, backendFailure)
				require.Empty(t, output.String())
			} else {
				require.NoError(t, err)
				require.Contains(t, output.String(), "Usage:")
				require.NotContains(t, output.String(), "configuration")
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
	openTUI = func(got context.Context, _ apptui.Overrides) error {
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

	for _, removed := range []string{"tui", "list", "report", "switch", "update"} {
		t.Run(removed, func(t *testing.T) {
			rootCmd.SetArgs([]string{removed, "dev"})

			_, err := rootCmd.ExecuteC()

			require.ErrorContains(t, err, `unknown command "`+removed+`"`)
		})
	}
}

func TestVersionFlagsDoNotOpenPicker(t *testing.T) {
	previousOpen := openTUI
	openTUI = func(context.Context, apptui.Overrides) error {
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
