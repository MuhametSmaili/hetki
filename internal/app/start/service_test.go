package start

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/logger"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/MuhametSmaili/hetki/internal/plan"
	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func acceptLayout(string) error { return nil }

type stubBackend struct {
	queryResult backend.StateResult
	queryErr    error
	layoutMatch func(string, string) bool
	dryRunLines []string
	applyErr    error
	onApply     func()

	queryCalls  int
	applyCalls  int
	attachCalls int
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

func (s *stubBackend) QueryState(context.Context) (backend.StateResult, error) {
	s.queryCalls++
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
	if s.onApply != nil {
		s.onApply()
	}
	return s.applyErr
}

func (s *stubBackend) DryRun(actions []backend.Action) ([]string, error) {
	s.dryRunCalls++
	s.lastActions = append([]backend.Action(nil), actions...)
	return append([]string(nil), s.dryRunLines...), nil
}

func (s *stubBackend) Attach(ctx context.Context, _ string) error {
	s.attachCalls++
	return ctx.Err()
}

func TestServiceRunRejectsInvalidTargetBeforeDetection(t *testing.T) {
	directory := t.TempDir()
	for _, test := range []struct {
		name string
		opts Options
		want error
	}{
		{"both", Options{Name: new("work"), Path: new("")}, nil},
		{"missing file", Options{Path: new(filepath.Join(directory, "missing.yaml"))}, os.ErrNotExist},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(func() (Backend, error) {
				t.Fatal("backend detected before target validation")
				return nil, nil
			}, acceptLayout)

			err := service.Run(t.Context(), test.opts)

			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
		})
	}
}

func TestServiceRunRejectsAuthoredLayoutBeforeDetection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	require.NoError(t, os.WriteFile(path, []byte("sessions:\n  - name: dev\n    windows:\n      - layout: opaque\n"), 0600))
	denied := errors.New("unsupported authored layout")

	for _, tc := range []struct {
		name   string
		policy func(string) error
		want   error
	}{
		{"missing policy", nil, manifest.ErrLayoutValidatorRequired},
		{"layout rejected", func(text string) error {
			if text != "opaque" {
				return errors.New("unexpected layout")
			}
			return denied
		}, denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(func() (Backend, error) {
				t.Fatal("backend detected before validation")
				return nil, nil
			}, tc.policy)

			err := service.Run(t.Context(), Options{Path: new(path)})

			require.ErrorIs(t, err, tc.want)
		})
	}
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
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) {
		return &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{{
			Name: "editor", Path: directory, Layout: "request", Panes: []manifest.Pane{{Path: directory}},
		}}}}}, "", nil
	}

	err := service.Run(t.Context(), Options{Path: new("./.hetki.yaml"), Force: true, DryRun: true})

	require.NoError(t, err)
	assert.Zero(t, stub.dryRunCalls, "recognized layouts must not plan a replacement")
}

func TestServiceRunCancellationAfterLoadPreventsBackendDetection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	detected := false
	service := Service{
		validateLayout: acceptLayout,
		LoadWorkspace: func(string) (*manifest.Workspace, string, error) {
			cancel()
			return &manifest.Workspace{}, "", nil
		},
		DetectBackend: func() (Backend, error) {
			detected = true
			return &stubBackend{}, nil
		},
	}

	err := service.Run(ctx, Options{Path: new("./.hetki.yaml")})

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, detected)
}

func TestServiceRunDryRunOutputsPlan(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hetki.yaml"), []byte(workspace), 0644))

	t.Chdir(tmpDir)

	stub := &stubBackend{dryRunLines: []string{"tmux new-session -d -s dev -n editor -c " + filepath.ToSlash(tmpDir)}}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

	output := captureLoggerOutput(t, func() {
		require.NoError(t, service.Run(context.Background(), Options{Path: new("./.hetki.yaml"), DryRun: true}))
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
		name   string
		detach bool
	}{
		{name: "attached"},
		{name: "detached", detach: true},
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
			service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) {
				return workspace, "/work/hetki/.hetki.yaml", nil
			}
			var err error

			output := captureLoggerOutput(t, func() {
				err = service.Run(t.Context(), Options{Path: new("./.hetki.yaml"), DryRun: true, Detach: tc.detach, Force: true, PruneSessions: true})
			})

			require.NoError(t, err)
			assert.Contains(t, output, "Workspace already up to date")
			assert.Contains(t, output, "Workspace: /work/hetki/.hetki.yaml")
			assert.Equal(t, 1, stub.queryCalls)
			assert.Zero(t, stub.applyCalls)
			assert.Zero(t, stub.attachCalls)
		})
	}
}

func TestServiceRunFailsWhenBackendStateQueryFails(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hetki.yaml"), []byte(workspace), 0644))

	t.Chdir(tmpDir)

	stub := &stubBackend{queryErr: errors.New("query failed")}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

	err := service.Run(context.Background(), Options{Path: new("./.hetki.yaml")})
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to query backend state: query failed")
	assert.Equal(t, "Check access to the tmux server and retry.", appshared.ErrorHint(err))
	assert.Zero(t, stub.dryRunCalls)
	assert.Zero(t, stub.applyCalls)
}

func TestServiceRunStampsWorkspacePathWhenPlanIsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := "sessions:\n  - name: dev\n    windows:\n      - name: editor\n        path: " + filepath.ToSlash(tmpDir) + "\n"
	path := filepath.Join(tmpDir, ".hetki.yaml")
	require.NoError(t, os.WriteFile(path, []byte(workspace), 0644))
	resolvedDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	expectedPath, err := canonicalPath(path)
	require.NoError(t, err)

	for _, test := range []struct {
		name        string
		stampErr    error
		detach      bool
		wantErr     error
		wantWarning bool
		wantAttach  int
	}{
		{name: "attached success", wantAttach: 1},
		{name: "attached cancellation", stampErr: context.Canceled, wantErr: context.Canceled, wantAttach: 1},
		{name: "attached failure during cancellation", stampErr: errors.Join(context.Canceled, errors.New("metadata denied")), wantErr: context.Canceled, wantWarning: true, wantAttach: 1},
		{name: "detached success", detach: true},
		{name: "detached cancellation", detach: true, stampErr: context.Canceled, wantErr: context.Canceled},
		{name: "detached failure during cancellation", detach: true, stampErr: errors.Join(context.Canceled, errors.New("metadata denied")), wantErr: context.Canceled, wantWarning: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stub := &stubBackend{queryResult: backend.StateResult{
				Sessions: []backend.Session{{
					Name: "dev",
					Windows: []backend.Window{{
						Name: "editor",
						Path: filepath.ToSlash(resolvedDir),
					}},
				}},
			}, applyErr: test.stampErr}
			if test.stampErr != nil {
				stub.onApply = cancel
			}
			service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
			var runErr error

			output := captureLoggerOutput(t, func() {
				runErr = service.Run(ctx, Options{Path: new(path), Detach: test.detach})
			})

			require.ErrorIs(t, runErr, test.wantErr)
			assert.Equal(t, test.wantWarning, strings.Contains(output, "workspace metadata not set"))
			assert.Equal(t, 1, stub.queryCalls)
			assert.Equal(t, 1, stub.applyCalls)
			assert.Equal(t, test.wantAttach, stub.attachCalls)
			require.Len(t, stub.lastActions, 1)
			action, ok := stub.lastActions[0].(backend.SetSessionOptionAction)
			require.True(t, ok)
			actualPath, err := canonicalPath(action.Value)
			require.NoError(t, err)
			assert.Equal(t, "dev", action.Session)
			assert.Equal(t, backend.WorkspacePathOption, action.Key)
			assert.Equal(t, expectedPath, actualPath)
		})
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

	t.Chdir(tmpDir)

	// Cold tmux: QueryState returns an empty StateResult, no error.
	stub := &stubBackend{queryResult: backend.StateResult{}}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)

	require.NoError(t, service.Run(context.Background(), Options{Path: new("./.hetki.yaml")}))

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
			service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

			require.NoError(t, service.Run(context.Background(), Options{Path: new("./.hetki.yaml"), Force: true, DryRun: dryRun}))
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
	_, err := toBackendActions([]plan.Action{unsupportedPlanAction{}}, false)
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
		plan.KillSessionAction{Name: "old", SessionID: "$2"},
		plan.KillWindowAction{Session: "dev", Window: "old-window", WindowID: "@7"},
	}

	converted, err := toBackendActions(actions, false)
	require.NoError(t, err)
	assert.Equal(t, []backend.Action{
		backend.CreateSessionAction{Name: "dev", WindowName: "editor", Path: "~/code"},
		backend.CreateWindowAction{Session: "dev", Name: "server", Path: "~/api"},
		backend.SplitPaneAction{Session: "dev", Window: "server", Path: "~/api"},
		backend.SendKeysAction{Session: "dev", Window: "server", Pane: 1, Command: "npm test"},
		backend.SelectLayoutAction{Session: "dev", Window: "server", Layout: "tiled"},
		backend.ZoomPaneAction{Session: "dev", Window: "server", Pane: 1},
		backend.KillSessionAction{Name: "$2"},
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
