package start

import (
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceRunSkipsOnlyEmptyObservedPaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{name: "known paths", paths: []string{"/work/project", "/work/tools"}},
		{name: "unknown first pane", paths: []string{"", "/work/tools"}},
		{name: "unknown later pane", paths: []string{"/work/project", ""}},
		{name: "all paths unknown", paths: []string{"", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []struct {
				name  string
				force bool
			}{
				{name: "merge"},
				{name: "force", force: true},
			} {
				t.Run(mode.name, func(t *testing.T) {
					window := observedWindow("editor", tc.paths)
					service, stub := reconciliationService(window)

					err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: mode.force})

					require.NoError(t, err)
					assert.Empty(t, stub.allActions, "unknown paths alone must not cause mutations or command replay")
					assert.Equal(t, 1, stub.attachCalls)
				})
			}
		})
	}
}

func TestServiceRunRetainsKnownDifferencesWithUnknownPaths(t *testing.T) {
	for _, tc := range []struct {
		name           string
		windowName     string
		paths          []string
		zoom           bool
		createsOnMerge bool
	}{
		{name: "missing pane", windowName: "editor", paths: []string{""}},
		{name: "extra pane", windowName: "editor", paths: []string{"", "", ""}},
		{name: "different later directory", windowName: "editor", paths: []string{"", "/other"}},
		{name: "different window directory", windowName: "editor", paths: []string{"/other", ""}, createsOnMerge: true},
		{name: "whitespace is not unknown", windowName: "editor", paths: []string{" ", ""}, createsOnMerge: true},
		{name: "different zoom", windowName: "editor", paths: []string{"", "/work/tools"}, zoom: true},
		{name: "different name", windowName: "other", paths: []string{"", ""}, createsOnMerge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []struct {
				name  string
				force bool
			}{
				{name: "merge"},
				{name: "force", force: true},
			} {
				t.Run(mode.name, func(t *testing.T) {
					window := observedWindow(tc.windowName, tc.paths)
					window.Panes[0].Zoom = tc.zoom
					service, stub := reconciliationService(window)
					var want []backend.Action
					if mode.force || tc.createsOnMerge {
						want = []backend.Action{
							backend.CreateWindowAction{Session: "dev", Name: "editor", Path: "/work/project"},
							backend.SplitPaneAction{Session: "dev", Window: "editor", Path: "/work/tools"},
							backend.SendKeysAction{Session: "dev", Window: "editor", Pane: 0, Command: "printf ready"},
						}
					}
					if mode.force {
						want = append(want, backend.KillWindowAction{Session: "dev", Window: tc.windowName, WindowID: "@1"})
					}

					err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: mode.force})

					require.NoError(t, err)
					assert.Equal(t, want, stub.lastActions)
				})
			}
		})
	}
}

func TestServiceRunKeepsCompatibleDuplicateWindows(t *testing.T) {
	for _, tc := range []struct {
		name           string
		firstPath      string
		reverse        bool
		zoomDifference bool
	}{
		{name: "unknown later path first", firstPath: "/work/project"},
		{name: "unknown later path last", firstPath: "/work/project", reverse: true},
		{name: "unknown window path first"},
		{name: "unknown window path last", reverse: true},
		{name: "known zoom difference last", firstPath: "/work/project", zoomDifference: true},
		{name: "known zoom difference first", firstPath: "/work/project", zoomDifference: true, reverse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			partial := observedWindow("editor", []string{tc.firstPath, ""})
			known := observedWindow("editor", []string{"/work/project", "/work/second"})
			known.ID = "@2"
			workspace := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{
				{Name: "editor", Path: "/work/project", Panes: []manifest.Pane{{Path: "/work/project", Command: "printf first"}, {Path: "/work/tools"}}},
				{Name: "editor", Path: "/work/project", Panes: []manifest.Pane{{Path: "/work/project", Command: "printf second"}, {Path: "/work/second"}}},
			}}}}
			if tc.zoomDifference {
				known.Panes[1].Path = "/work/tools"
				known.Panes[0].Zoom = true
				workspace.Sessions[0].Windows[1].Panes[1].Path = "/work/tools"
				workspace.Sessions[0].Windows[1].Panes[0].Zoom = true
			}
			windows := []backend.Window{partial, known}
			if tc.reverse {
				windows[0], windows[1] = windows[1], windows[0]
			}
			for _, mode := range []struct {
				name  string
				force bool
			}{
				{name: "merge"},
				{name: "force", force: true},
			} {
				t.Run(mode.name, func(t *testing.T) {
					stub := &stubBackend{queryResult: backend.StateResult{Sessions: []backend.Session{{Name: "dev", Windows: windows}}}}
					service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
					service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

					err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: mode.force})

					require.NoError(t, err)
					assert.Empty(t, stub.allActions, "a conflicting known path must not displace a compatible partial observation")
				})
			}
		})
	}
}

func observedWindow(name string, paths []string) backend.Window {
	window := backend.Window{ID: "@1", Name: name, Path: paths[0]}
	for _, path := range paths {
		window.Panes = append(window.Panes, backend.Pane{Path: path})
	}
	return window
}

func reconciliationService(window backend.Window) (Service, *stubBackend) {
	stub := &stubBackend{queryResult: backend.StateResult{
		Sessions: []backend.Session{{Name: "dev", Windows: []backend.Window{window}}},
	}}
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{
		Name: "dev", Windows: []manifest.Window{{
			Name: "editor", Path: "/work/project",
			Panes: []manifest.Pane{
				{Path: "/work/project", Command: "printf ready"},
				{Path: "/work/tools"},
			},
		}},
	}}}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }
	return service, stub
}
