package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func privateConfig(t testing.TB, contents string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, err := config.DefaultPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0600))
	return path
}

func TestPreparedServiceUsesConfigSnapshot(t *testing.T) {
	path := privateConfig(t, "ui: {view: tree, scope: tracked, preview: false, preview_width: 45}\nkeymap: {normal: {toggle_preview: [p]}}")
	stub := &stubBackend{state: liveState()}
	detections := 0
	service, err := prepareService(t.Context(), func() (Backend, error) {
		detections++
		return stub, nil
	}, Overrides{})
	require.NoError(t, err)
	require.Zero(t, detections)
	require.Empty(t, service.Notice)
	runs := 0
	service.RunUI = func(_ context.Context, snapshot list.Snapshot, options ui.Options, runtime ui.Runtime) (ui.Result, error) {
		runs++
		require.Equal(t, ui.State{View: ui.ViewTree, Scope: ui.ScopeTracked, Preview: false}, options.Initial)
		require.Equal(t, 45, options.PreviewWidth)
		require.Equal(t, []string{"p"}, options.Keys.Keys(ui.KeyModeNormal, ui.ActionTogglePreview))
		require.Len(t, snapshot.Items, 1)
		require.Len(t, snapshot.Items[0].Children, 1)
		require.Equal(t, 1, stub.queryCalls, "initial tree needs one inventory query")
		require.NoError(t, os.WriteFile(path, []byte("ui: {view: invalid}"), 0600))

		refreshed, err := runtime.Dispatch(ui.ActionRequest{ActionID: ui.ActionRefresh})
		require.NoError(t, err)
		require.Len(t, refreshed.Snapshot.Items[0].Children, 1)
		require.Equal(t, 2, stub.queryCalls)
		toggled, err := runtime.Dispatch(ui.ActionRequest{ActionID: ui.ActionToggleProjection})
		require.NoError(t, err)
		require.Equal(t, ui.ViewFlat, toggled.View)
		require.Equal(t, 2, stub.queryCalls, "toggle uses the loaded inventory")
		require.Equal(t, []string{"p"}, service.Options.Keys.Keys(ui.KeyModeNormal, ui.ActionTogglePreview), "configuration is not reread")
		return ui.Result{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
	require.Equal(t, 1, runs)
	require.Equal(t, 1, detections)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "ui: {view: invalid}", string(contents), "no automatic writes")
}

func TestOpenCancellationIsFatalBeforeBackend(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			privateConfig(t, "ui: [malformed")
			ctx, cancel := context.WithCancel(t.Context())
			if errors.Is(cause, context.DeadlineExceeded) {
				cancel()
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			} else {
				cancel()
			}
			defer cancel()

			err := Open(ctx, func() (Backend, error) {
				t.Fatal("cancellation must abort before backend detection")
				return nil, nil
			}, Overrides{})

			require.ErrorIs(t, err, cause)
		})
	}
}

func TestPreparedServiceIgnoresBrokenConfig(t *testing.T) {
	for _, tt := range []struct {
		name, contents string
		setup          func(*testing.T, string)
	}{
		{name: "malformed YAML", contents: "ui: [malformed"},
		{name: "wrong preview type discards valid fields", contents: "ui: {view: tree, scope: tracked, preview: \"secret\\e[31m\"}"},
		{name: "wrong width type discards valid fields", contents: "ui: {view: tree, scope: tracked, preview: true, preview_width: \"secret\\e[31m\"}"},
		{name: "malformed overflow width discards valid fields", contents: "ui: {view: tree, preview: true, preview_width: 0" + strings.Repeat("7", 100) + "9}"},
		{name: "duplicate width discards valid fields", contents: "ui: {view: tree, preview: true, preview_width: 0, preview_width: 45}"},
		{name: "wrong keymap type discards valid fields", contents: "ui: {view: tree}\nkeymap: {normal: {quit: false}}"},
		{name: "remembering remains unsupported", contents: "ui: {preview_width: 45, remember_state: true}"},
		{name: "wrong type discards valid fields", contents: "ui: {view: tree, scope: null, preview: true}"},
		{name: "nonregular file", setup: func(t *testing.T, path string) {
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Mkdir(path, 0700))
		}},
		{name: "path discovery", setup: func(t *testing.T, _ string) {
			t.Setenv("HOME", "")
			t.Setenv("XDG_CONFIG_HOME", "")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := privateConfig(t, tt.contents)
			if tt.setup != nil {
				tt.setup(t, path)
			}
			stub := &stubBackend{state: liveState()}

			service, err := prepareService(t.Context(), func() (Backend, error) { return stub, nil }, Overrides{})

			require.NoError(t, err)
			require.Equal(t, ui.DefaultOptions(), service.Options)
			require.Contains(t, service.Notice, "configuration ignored")
			require.Contains(t, service.Notice, "defaults")
			require.NotContains(t, service.Notice, "secret")
			require.NotContains(t, service.Notice, "\x1b")
			service.RunUI = func(_ context.Context, snapshot list.Snapshot, _ ui.Options, _ ui.Runtime) (ui.Result, error) {
				require.Contains(t, snapshot.Notice, service.Notice)
				return ui.Result{}, nil
			}
			require.NoError(t, service.Run(t.Context()))
			require.Equal(t, 1, stub.queryCalls)
		})
	}
}

func TestPreparedServiceMissingAndEmptyConfigAreQuiet(t *testing.T) {
	for _, contents := range []string{"", "# comment", "{}", "ui: {}", "missing"} {
		t.Run(contents, func(t *testing.T) {
			path := privateConfig(t, contents)
			if contents == "missing" {
				require.NoError(t, os.Remove(path))
			}

			service, err := prepareService(t.Context(), nil, Overrides{})

			require.NoError(t, err)
			require.Equal(t, ui.DefaultOptions(), service.Options)
			require.Empty(t, service.Notice)
		})
	}
}

func TestPreparedServiceStayOpenRequiresTmux(t *testing.T) {
	for _, tt := range []struct {
		name     string
		tmux     string
		stayOpen bool
		wantErr  bool
	}{
		{name: "enabled inside tmux", tmux: "isolated,1,0", stayOpen: true},
		{name: "enabled outside tmux", stayOpen: true, wantErr: true},
		{name: "default outside tmux"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			privateConfig(t, "")
			t.Setenv("TMUX", tt.tmux)

			service, err := prepareService(t.Context(), nil, Overrides{StayOpen: tt.stayOpen})

			if tt.wantErr {
				require.ErrorContains(t, err, "--stay-open requires running inside tmux")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.stayOpen, service.StayOpen)
			require.Equal(t, ui.DefaultOptions(), service.Options)
		})
	}
}

func TestOpenSanitizesBackendDiagnosticsAndPreservesCause(t *testing.T) {
	privateConfig(t, "ui: {view: invalid}")
	path := "backend\x1b[31m\n/socket"
	failure := &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}

	err := Open(t.Context(), func() (Backend, error) { return nil, fmt.Errorf("detect: %w", failure) }, Overrides{})

	require.ErrorIs(t, err, syscall.EACCES)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Same(t, failure, pathError)
	require.Equal(t, terminal.Sanitize(err.Error()), err.Error())
	require.Contains(t, err.Error(), "backend")
}

func TestPreparedServiceSanitizesAndBoundsReadWarning(t *testing.T) {
	for _, name := range []string{"unsafe symlink", "oversize zero-width path"} {
		t.Run(name, func(t *testing.T) {
			privateConfig(t, "")
			suffix := "unsafe\x1b[31m\n"
			if name == "oversize zero-width path" {
				suffix += strings.Repeat("\u0301", 4096)
			}
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), suffix))
			if name == "unsafe symlink" {
				path, err := config.DefaultPath()
				require.NoError(t, err)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				require.NoError(t, os.Symlink(path, path))
			}

			service, err := prepareService(t.Context(), nil, Overrides{})

			require.NoError(t, err)
			require.Equal(t, ui.DefaultOptions(), service.Options)
			require.Contains(t, service.Notice, "configuration ignored")
			require.NotContains(t, service.Notice, "\x1b")
			require.NotContains(t, service.Notice, "\n")
			require.LessOrEqual(t, terminal.Width(service.Notice), 512)
			require.Less(t, len(service.Notice), 4096)
		})
	}
}

type initialNoticeDriver struct {
	openingStatusDriver
}

func (d *initialNoticeDriver) Load(ctx context.Context, view ui.ViewMode) (list.Snapshot, error) {
	snapshot, err := d.switchDriver.Load(ctx, view)
	snapshot.Notice = "frecency state unavailable"
	return snapshot, err
}

func TestPreparedServiceNoticeReachesUIAlongsideDriverAndStatus(t *testing.T) {
	privateConfig(t, "ui: {view: wrong, scope: tracked, preview: true, preview_width: 18446744073709551616}")
	service, err := prepareService(t.Context(), nil, Overrides{})
	require.NoError(t, err)
	driver := &initialNoticeDriver{openingStatusDriver: openingStatusDriver{err: fmt.Errorf("status fixture unavailable")}}
	service.Driver = driver
	service.RunUI = func(_ context.Context, snapshot list.Snapshot, options ui.Options, _ ui.Runtime) (ui.Result, error) {
		require.Equal(t, ui.State{View: ui.ViewFlat, Scope: ui.ScopeTracked, Preview: true}, options.Initial)
		require.Equal(t, 70, options.PreviewWidth)
		require.True(t, strings.HasPrefix(snapshot.Notice, "configuration warning: ui.view"))
		require.Contains(t, snapshot.Notice, "ui.preview_width: allowed 1–99; invalid preference ignored")
		require.NotContains(t, snapshot.Notice, "18446744073709551616")
		require.NotContains(t, snapshot.Notice, "wrong")
		require.Contains(t, snapshot.Notice, "frecency state unavailable")
		require.Contains(t, snapshot.Notice, "status unavailable: status fixture unavailable")
		return ui.Result{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
	require.Equal(t, 1, driver.loads)
}

func TestDirectServiceIgnoresPersonalConfig(t *testing.T) {
	privateConfig(t, "ui: {view: invalid}")
	stub := &stubBackend{state: liveState()}
	service := NewService(func() (Backend, error) { return stub, nil })
	service.RunUI = func(_ context.Context, _ list.Snapshot, options ui.Options, _ ui.Runtime) (ui.Result, error) {
		require.Equal(t, ui.DefaultOptions(), options)
		return ui.Result{}, nil
	}

	require.NoError(t, service.Run(t.Context()))
	require.Equal(t, 1, stub.queryCalls)
}
