package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceCommandsRejectInvalidSelectorsBeforeDetection(t *testing.T) {
	previous := detectBackend
	detectBackend = func(...string) (backend.Backend, error) {
		t.Fatal("backend detection before selector validation")
		return nil, nil
	}
	t.Cleanup(func() { detectBackend = previous })

	for _, command := range []string{"start", "save"} {
		for _, test := range []struct {
			name string
			args []string
			want string
		}{
			{"missing", nil, "choose exactly one workspace target"},
			{"extra name", []string{"work", "other"}, "accepts at most one workspace name"},
			{"empty name", []string{""}, "workspace name must not be empty"},
			{"positional path", []string{"./work.yaml"}, "workspace name must be one filename"},
			{"empty file", []string{"--file", ""}, "--file must not be empty"},
			{"both selectors", []string{"work", "--file", "./work.yaml"}, "cannot combine a workspace name with --file"},
			{"file before name", []string{"-f", "./work.yaml", "work"}, "cannot combine a workspace name with --file"},
			{"empty second selector", []string{"work", "--file="}, "cannot combine a workspace name with --file"},
		} {
			t.Run(command+"/"+test.name, func(t *testing.T) {
				err := executeWorkspaceCommand(t, append([]string{command}, test.args...)...)

				require.ErrorContains(t, err, test.want)
				require.Equal(t, fmt.Sprintf("Use hetki %s work or hetki %s --file ./work.yaml.", command, command), appshared.ErrorHint(err))
			})
		}
	}
}

func TestWorkspaceSelectorsRoundTripThroughCommands(t *testing.T) {
	for _, test := range []struct {
		name, flag, value, destination string
	}{
		{"literal name", "", "$HETKI_NAME", ".config/hetki/workspaces/$HETKI_NAME.yml"},
		{"relative file", "--file", "./work.yaml", "work.yaml"},
		{"short file", "-f", "./work.yaml", "work.yaml"},
		{"absolute file", "--file", "", "absolute.yaml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Chdir(home)
			t.Setenv("HOME", home)
			t.Setenv("HETKI_NAME", "unexpected")
			destination := filepath.Join(home, test.destination)
			require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0700))
			if test.flag == "" {
				writeWorkspaceFile(t, filepath.Dir(destination), filepath.Base(destination), "sessions:\n  - name: dev\n    windows:\n      - name: editor\n")
				writeWorkspaceFile(t, home, test.value, "not a workspace")
			}
			value := test.value
			if value == "" {
				value = destination
			}
			stub := &stubBackend{queryResult: backend.StateResult{
				Sessions: []backend.Session{{Name: "dev", Windows: []backend.Window{{Name: "editor", Path: home}}}},
				Active:   backend.ActiveContext{Session: "dev"},
			}}
			withStubBackend(t, stub)
			selector := []string{value}
			if test.flag != "" {
				selector = []string{test.flag, value}
			}

			captureLoggerOutput(t, func() {
				require.NoError(t, executeWorkspaceCommand(t, append([]string{"save"}, selector...)...))
			})

			data, err := os.ReadFile(destination)
			require.NoError(t, err)
			require.Contains(t, string(data), "name: dev")
			stub.queryResult = backend.StateResult{}

			captureLoggerOutput(t, func() {
				require.NoError(t, executeWorkspaceCommand(t, append([]string{"start", "-n"}, selector...)...))
			})

			require.Zero(t, stub.applyCalls)
			require.Zero(t, stub.attachCalls)
			require.Len(t, stub.lastActions, 1)
			action, ok := stub.lastActions[0].(backend.CreateSessionAction)
			require.True(t, ok, "unexpected action: %T", stub.lastActions[0])
			require.Equal(t, "dev", action.Name)
		})
	}
}

func TestStartMissingNamedWorkspaceDoesNotUseLocalFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	for _, name := range []string{"work.yaml", ".hetki.yaml"} {
		writeWorkspaceFile(t, home, name, "sessions:\n  - name: dev\n    windows:\n      - name: editor\n")
	}
	previous := detectBackend
	detectBackend = func(...string) (backend.Backend, error) {
		t.Fatal("missing named workspace reached backend detection")
		return nil, nil
	}
	t.Cleanup(func() { detectBackend = previous })

	err := executeWorkspaceCommand(t, "start", "work", "-n")

	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestWorkspaceCompletion(t *testing.T) {
	home := filepath.Join(t.TempDir(), "$HETKI_HOME")
	t.Setenv("HOME", home)
	t.Setenv("HETKI_HOME", "wrong-directory")
	directory := filepath.Join(home, ".config", "hetki", "workspaces")
	require.NoError(t, os.MkdirAll(directory, 0700))
	for _, name := range []string{"work.yml", "work.v1.yaml", "work.yaml.yml", "work .yaml", ".yaml"} {
		writeWorkspaceFile(t, directory, name, "")
	}

	for _, command := range []string{"start", "save"} {
		for _, test := range []struct {
			name string
			args []string
			want string
		}{
			{"name", []string{""}, "work\nwork .yaml\nwork.v1.yaml\nwork.yaml.yml\n:4\n"},
			{"file", []string{"--file", ""}, "yaml\nyml\n:8\n"},
			{"short file", []string{"-f", ""}, "yaml\nyml\n:8\n"},
			{"name already selected", []string{"work", ""}, ":4\n"},
			{"file already selected", []string{"--file", "./work.yaml", ""}, ":4\n"},
			{"empty file selected", []string{"--file=", ""}, ":4\n"},
		} {
			t.Run(command+"/"+test.name, func(t *testing.T) {
				var output bytes.Buffer
				rootCmd.SetOut(&output)
				t.Cleanup(func() { rootCmd.SetOut(nil) })

				err := executeWorkspaceCommand(t, append([]string{"__complete", command}, test.args...)...)

				require.NoError(t, err)
				require.Equal(t, test.want, output.String())
			})
		}
	}
}

func TestWorkspaceCommandsRejectRetiredFlagsBeforeDetection(t *testing.T) {
	previous := detectBackend
	detectBackend = func(...string) (backend.Backend, error) {
		t.Fatal("retired flag reached backend detection")
		return nil, nil
	}
	t.Cleanup(func() { detectBackend = previous })

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"start path", []string{"start", "--path", "./work.yaml"}, "unknown flag: --path"},
		{"save path", []string{"save", "--path", "./work.yaml"}, "unknown flag: --path"},
		{"start short path", []string{"start", "-p", "./work.yaml"}, "unknown shorthand flag: 'p'"},
		{"save short path", []string{"save", "-p", "./work.yaml"}, "unknown shorthand flag: 'p'"},
		{"start name flag", []string{"start", "--name", "work"}, "unknown flag: --name"},
		{"save name flag", []string{"save", "--name", "work"}, "unknown flag: --name"},
		{"start bare former force", []string{"start", "work", "-f"}, "flag needs an argument: 'f'"},
		{"save old name", []string{"save", "-n", "work"}, "unknown shorthand flag: 'n'"},
		{"save all", []string{"save", "work", "--all"}, "unknown flag: --all"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := executeWorkspaceCommand(t, test.args...)

			require.ErrorContains(t, err, test.want)
		})
	}
}

func executeWorkspaceCommand(t *testing.T, args ...string) error {
	t.Helper()
	resetCommandGlobals()
	rootCmd.SetArgs(args)
	rootCmd.SetContext(t.Context())
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetContext(context.Background())
		resetCommandGlobals()
	})
	_, err := rootCmd.ExecuteC()
	return err
}
