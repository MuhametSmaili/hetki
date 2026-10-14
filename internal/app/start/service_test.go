package start

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/logger"
	"github.com/MSmaili/hetki/internal/manifest"
	"github.com/MSmaili/hetki/internal/plan"
	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubBackend struct {
	queryResult backend.StateResult
	queryErr    error
	layoutMatch func(string, string) bool
	dryRunLines []string
	applyErr    error

	applyCalls  int
	attachCalls int
	switchCalls int
	dryRunCalls int
	lastActions []backend.Action
	allActions  [][]backend.Action
}

func (s *stubBackend) LayoutsMatch(desired, actual string) bool {
	if s.layoutMatch != nil {
		return s.layoutMatch(desired, actual)
	}
	return desired == actual
}
func (s *stubBackend) LayoutPreset(observed string) (string, error) { return observed, nil }

func (s *stubBackend) Name() string { return "stub" }
func (s *stubBackend) CapturePane(context.Context, backend.Target) (string, error) {
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
	copied := append([]backend.Action(nil), actions...)
	s.lastActions = copied
	s.allActions = append(s.allActions, copied)
	return s.applyErr
}

func (s *stubBackend) DryRun(actions []backend.Action) ([]string, error) {
	s.dryRunCalls++
	s.lastActions = append([]backend.Action(nil), actions...)
	return append([]string(nil), s.dryRunLines...), nil
}

func (s *stubBackend) Attach(context.Context, string) error {
	s.attachCalls++
	return nil
}

func (s *stubBackend) Switch(context.Context, backend.Target) error {
	s.switchCalls++
	return nil
}

func TestServiceRunUsesBackendLayoutPolicy(t *testing.T) {
	directory := t.TempDir()
	stub := &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{ID: "$1", Name: "dev", Windows: []backend.Window{{
			ID: "@1", Name: "editor", Path: directory, Layout: "observation",
			Panes: []backend.Pane{{Path: directory}},
		}}}},
	}}
	stub.layoutMatch = func(request, observation string) bool {
		assert.Equal(t, "request", request)
		assert.Equal(t, "observation", observation)
		return true
	}
	service := NewService(func(...string) (backend.Backend, error) { return stub, nil })
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) {
		return &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{{
			Name: "editor", Path: directory, Layout: "request", Panes: []manifest.Pane{{Path: directory}},
		}}}}}, "", nil
	}

	err := service.Run(t.Context(), Options{Force: true, DryRun: true})

	require.NoError(t, err)
	assert.Zero(t, stub.dryRunCalls, "recognized layouts must not plan a replacement")
}

func TestServiceRunCancellationAfterLoadPreventsBackendDetection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	detected := false
	service := Service{
		LoadWorkspace: func(string) (*manifest.Workspace, string, error) {
			cancel()
			return &manifest.Workspace{}, "", nil
		},
		DetectBackend: func(...string) (backend.Backend, error) {
			detected = true
			return &stubBackend{}, nil
		},
	}

	err := service.Run(ctx, Options{})

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, detected)
}

func TestServiceRunDryRunOutputsPlan(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hetki.yaml"), []byte(workspace), 0644))

	previousWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(previousWD))
	})

	stub := &stubBackend{dryRunLines: []string{"tmux new-session -d -s dev -n editor -c " + filepath.ToSlash(tmpDir)}}
	service := NewService(func(...string) (backend.Backend, error) { return stub, nil })

	output := captureLoggerOutput(t, func() {
		require.NoError(t, service.Run(context.Background(), Options{DryRun: true}))
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

func TestServiceRunDryRunIsReadOnlyWhenPlanIsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		force bool
	}{
		{name: "merge"},
		{name: "force", force: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := &manifest.Workspace{Sessions: []manifest.Session{{
				Name:    "dev",
				Windows: []manifest.Window{{Name: "editor", Path: "/work/hetki"}},
			}}}
			stub := &stubBackend{queryResult: backend.StateResult{
				Sessions: []backend.Session{{
					Name:    "dev",
					Windows: []backend.Window{{Name: "editor", Path: "/work/hetki"}},
				}},
			}}
			service := NewService(func(...string) (backend.Backend, error) { return stub, nil })
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) {
				return workspace, "/work/hetki/.hetki.yaml", nil
			}
			var err error

			output := captureLoggerOutput(t, func() {
				err = service.Run(context.Background(), Options{DryRun: true, Force: tc.force})
			})

			require.NoError(t, err)
			assert.Contains(t, output, "Workspace already up to date")
			assert.Zero(t, stub.applyCalls)
			assert.Zero(t, stub.attachCalls)
			assert.Zero(t, stub.switchCalls)
		})
	}
}

func TestServiceRunFailsWhenBackendStateQueryFails(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hetki.yaml"), []byte(workspace), 0644))

	previousWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(previousWD))
	})

	stub := &stubBackend{queryErr: errors.New("query failed")}
	service := NewService(func(...string) (backend.Backend, error) { return stub, nil })

	err = service.Run(context.Background(), Options{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to query backend state: query failed")
	assert.ErrorContains(t, err, "Verify tmux is running and retry")
	assert.Zero(t, stub.dryRunCalls)
	assert.Zero(t, stub.applyCalls)
}

func TestServiceRunStampsWorkspacePathWhenPlanIsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hetki.yaml"), []byte(workspace), 0644))

	previousWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(previousWD))
	})

	resolvedDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	stub := &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{
			Name: "dev",
			Windows: []backend.Window{{
				Name: "editor",
				Path: filepath.ToSlash(resolvedDir),
			}},
		}},
	}}
	service := NewService(func(...string) (backend.Backend, error) { return stub, nil })

	require.NoError(t, service.Run(context.Background(), Options{}))
	assert.Equal(t, 1, stub.applyCalls)
	assert.Equal(t, 1, stub.attachCalls)
	expectedPath, err := canonicalPath(filepath.Join(tmpDir, ".hetki.yaml"))
	require.NoError(t, err)
	if assert.Len(t, stub.lastActions, 1) {
		action, ok := stub.lastActions[0].(backend.SetSessionOptionAction)
		if assert.True(t, ok) {
			actualPath, err := canonicalPath(action.Value)
			require.NoError(t, err)
			assert.Equal(t, "dev", action.Session)
			assert.Equal(t, backend.WorkspacePathOption, action.Key)
			assert.Equal(t, expectedPath, actualPath)
		}
	}
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func TestServiceRunColdStartAppliesCreateActions(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hetki.yaml"), []byte(workspace), 0644))

	previousWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(previousWD))
	})

	// Cold tmux: QueryState returns an empty StateResult, no error.
	stub := &stubBackend{queryResult: backend.StateResult{}}
	service := NewService(func(...string) (backend.Backend, error) { return stub, nil })

	require.NoError(t, service.Run(context.Background(), Options{}))

	// Two Apply calls: first the plan, second the workspace-path metadata stamp.
	require.Equal(t, 2, stub.applyCalls)
	assert.Equal(t, 1, stub.attachCalls)

	// First call should carry the create-session plan action.
	planActions := stub.allActions[0]
	if assert.Len(t, planActions, 1) {
		action, ok := planActions[0].(backend.CreateSessionAction)
		if assert.True(t, ok) {
			assert.Equal(t, "dev", action.Name)
			assert.Equal(t, "editor", action.WindowName)
		}
	}

	// Second call stamps the workspace path metadata.
	stampActions := stub.allActions[1]
	if assert.Len(t, stampActions, 1) {
		assert.IsType(t, backend.SetSessionOptionAction{}, stampActions[0])
	}
}

func TestServiceRunForcePreservesUnrelatedSessionsInNormalAndDryRun(t *testing.T) {
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{
		Name:    "dev",
		Windows: []manifest.Window{{Name: "editor", Path: "/work/hetki"}},
	}}}
	live := backend.StateResult{Sessions: []backend.Session{
		{Name: "dev", Windows: []backend.Window{
			{ID: "@1", Name: "editor", Path: "/work/hetki"},
			{ID: "@9", Name: "scratch", Path: "/tmp"},
		}},
		{Name: "personal", Windows: []backend.Window{{Name: "shell", Path: "/home/user"}}},
	}}
	want := []backend.Action{backend.KillWindowAction{Session: "dev", Window: "scratch", WindowID: "@9"}}

	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "dry-run"}[dryRun], func(t *testing.T) {
			stub := &stubBackend{queryResult: live, dryRunLines: []string{"tmux kill-window -t @9"}}
			service := NewService(func(...string) (backend.Backend, error) { return stub, nil })
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

			require.NoError(t, service.Run(context.Background(), Options{Force: true, DryRun: dryRun}))
			assert.Equal(t, want, stub.lastActions)
			for _, action := range stub.lastActions {
				_, killsSession := action.(backend.KillSessionAction)
				assert.False(t, killsSession)
			}
			if dryRun {
				assert.Equal(t, 1, stub.dryRunCalls)
				assert.Zero(t, stub.applyCalls)
				assert.Zero(t, stub.attachCalls)
			} else {
				assert.Zero(t, stub.dryRunCalls)
				assert.Equal(t, 1, stub.applyCalls)
				assert.Equal(t, 1, stub.attachCalls)
			}
		})
	}
}

type unsupportedPlanAction struct{}

func (unsupportedPlanAction) Comment() string { return "unsupported" }
func (unsupportedPlanAction) Validate() error { return nil }

func TestToBackendActionsRejectsUnsupportedActions(t *testing.T) {
	_, err := toBackendActions([]plan.Action{unsupportedPlanAction{}})
	require.ErrorContains(t, err, "unsupported plan action")
}

func TestToBackendActionsMapsPlannerActions(t *testing.T) {
	actions := []plan.Action{
		plan.CreateSessionAction{Name: "dev", WindowName: "editor", Path: "~/code"},
		plan.CreateWindowAction{Session: "dev", Name: "server", Path: "~/api"},
		plan.SplitPaneAction{Session: "dev", Window: "server", Path: "~/api"},
		plan.SendKeysAction{Session: "dev", Window: "server", Pane: 1, Command: "npm test"},
		plan.SelectLayoutAction{Session: "dev", Window: "server", Layout: "tiled"},
		plan.ZoomPaneAction{Session: "dev", Window: "server", Pane: 1},
		plan.KillSessionAction{Name: "old"},
		plan.KillWindowAction{Session: "dev", Window: "old-window", WindowID: "@7"},
	}

	converted, err := toBackendActions(actions)
	require.NoError(t, err)
	assert.Equal(t, []backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor", Path: "~/code"},
		backend.CreateWindowAction{Session: "dev", Name: "server", Path: "~/api"},
		backend.SplitPaneAction{Session: "dev", Window: "server", Path: "~/api"},
		backend.SendKeysAction{Session: "dev", Window: "server", Pane: 1, Command: "npm test"},
		backend.SelectLayoutAction{Session: "dev", Window: "server", Layout: "tiled"},
		backend.ZoomPaneAction{Session: "dev", Window: "server", Pane: 1},
		backend.KillSessionAction{Name: "old"},
		backend.KillWindowAction{Session: "dev", Window: "old-window", WindowID: "@7"},
	}, converted)
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
