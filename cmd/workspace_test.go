package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

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
			{"positional name", []string{"work"}, "unknown command"},
			{"empty name", []string{"--name="}, "--name must not be empty"},
			{"empty path", []string{"--path", ""}, "--path must not be empty"},
			{"both selectors", []string{"-n", "work", "-p", "./work.yaml"}, "cannot use both"},
			{"empty second selector", []string{"-n", "work", "--path="}, "cannot use both"},
		} {
			t.Run(command+"/"+test.name, func(t *testing.T) {
				err := executeWorkspaceCommand(t, append([]string{command}, test.args...)...)

				require.ErrorContains(t, err, test.want)
			})
		}
	}
}

func TestWorkspaceSelectorsRoundTripThroughCommands(t *testing.T) {
	for _, test := range []struct {
		name, flag, value, destination string
	}{
		{"literal name", "--name", "$HETKI_NAME", ".config/hetki/workspaces/$HETKI_NAME.yml"},
		{"relative file", "-p", "./work.yaml", "work.yaml"},
		{"absolute file", "--path", "", "absolute.yaml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Chdir(home)
			t.Setenv("HOME", home)
			t.Setenv("HETKI_NAME", "unexpected")
			destination := filepath.Join(home, test.destination)
			require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0700))
			if test.flag == "-n" || test.flag == "--name" {
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

			captureLoggerOutput(t, func() {
				require.NoError(t, executeWorkspaceCommand(t, "save", test.flag, value))
			})

			data, err := os.ReadFile(destination)
			require.NoError(t, err)
			require.Contains(t, string(data), "name: dev")
			stub.queryResult = backend.StateResult{}

			captureLoggerOutput(t, func() {
				require.NoError(t, executeWorkspaceCommand(t, "start", test.flag, value, "--dry-run"))
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

func TestWorkspaceFlagCompletion(t *testing.T) {
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
			{"name", []string{"--name", ""}, "work\nwork .yaml\nwork.v1.yaml\nwork.yaml.yml\n:4\n"},
			{"path", []string{"--path", ""}, "yaml\nyml\n:8\n"},
			{"no positional completion", []string{""}, ":4\n"},
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
