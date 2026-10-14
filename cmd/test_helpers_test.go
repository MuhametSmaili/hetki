package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/logger"
	"github.com/fatih/color"
	"github.com/stretchr/testify/require"
)

func TestLoggerSanitizesExternalArgumentsBeforeStyling(t *testing.T) {
	output := captureLoggerOutput(t, func() {
		logger.Plain("error: %v", errors.New("failure"))
		logger.Plain("name: %s", "dev\x1b[31m-red\x1b[0m\nnext")
	})
	require.Equal(t, "error: failure\nname: dev-red\\nnext\n", output)
}

type stubBackend struct {
	queryResult backend.StateResult
	queryErr    error
	dryRunLines []string
	applyErr    error

	applyCalls  int
	attachCalls int
	dryRunCalls int
	lastActions []backend.Action
	lastAttach  string
}

func (s *stubBackend) LayoutsMatch(desired, actual string) bool     { return desired == actual }
func (s *stubBackend) LayoutPreset(observed string) (string, error) { return observed, nil }

func (s *stubBackend) Name() string { return "stub" }
func (s *stubBackend) CapturePane(context.Context, string) (string, error) {
	panic("unexpected pane capture")
}

func (s *stubBackend) QueryState(context.Context) (backend.StateResult, error) {
	if s.queryErr != nil {
		return backend.StateResult{}, s.queryErr
	}
	return s.queryResult, nil
}

func (s *stubBackend) Apply(_ context.Context, actions []backend.Action) error {
	s.applyCalls++
	s.lastActions = append([]backend.Action(nil), actions...)
	return s.applyErr
}

func (s *stubBackend) DryRun(actions []backend.Action) ([]string, error) {
	s.dryRunCalls++
	s.lastActions = append([]backend.Action(nil), actions...)
	return append([]string(nil), s.dryRunLines...), nil
}

func (s *stubBackend) Attach(_ context.Context, session string) error {
	s.attachCalls++
	s.lastAttach = session
	return nil
}

func (s *stubBackend) Switch(context.Context, string) error {
	return nil
}

func resetCommandGlobals() {
	saveCmd.SetContext(context.Background())
	startCmd.SetContext(context.Background())
	dryRun = false
	force = false
	savePath = ""
	saveName = ""
	saveAll = false
}

func withStubBackend(t *testing.T, stub backend.Backend) {
	t.Helper()
	previous := detectBackend
	detectBackend = func(name ...string) (backend.Backend, error) {
		return stub, nil
	}
	t.Cleanup(func() {
		detectBackend = previous
	})
}

func captureLoggerOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	previousOutput := logger.SetOutput(&buf)
	previousNoColor := color.NoColor
	color.NoColor = true
	defer func() {
		logger.SetOutput(previousOutput)
		color.NoColor = previousNoColor
	}()

	fn()
	return buf.String()
}

func writeWorkspaceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
