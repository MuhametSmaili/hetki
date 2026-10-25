package start

import (
	"errors"
	"slices"
	"testing"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestForceRejectsLinkedWindowDestructionBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		prune        bool
		dryRun       bool
		replace      bool
		declareOther bool
	}{
		{name: "extra window"},
		{name: "extra window dry run", dryRun: true},
		{name: "extra window with session pruning", prune: true},
		{name: "extra window with session pruning dry run", prune: true, dryRun: true},
		{name: "mismatched window", replace: true},
		{name: "both linked sessions declared", declareOther: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, stub := pruningService()
			shared := stub.queryResult.Sessions[1].Windows[0]
			stub.queryResult.Sessions[0].Windows = append(stub.queryResult.Sessions[0].Windows, shared)
			slices.Reverse(stub.queryResult.Sessions)
			workspace := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{
				{Name: "editor", Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
				{Name: "tools", Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
			}}}}
			if tc.replace {
				workspace.Sessions[0].Windows = append(workspace.Sessions[0].Windows, manifest.Window{
					Name: shared.Name, Path: "/work", Panes: []manifest.Pane{{Path: "/work"}, {Path: "/work"}},
				})
			}
			if tc.declareOther {
				workspace.Sessions = append(workspace.Sessions, manifest.Session{Name: "old", Windows: []manifest.Window{
					{Name: shared.Name, Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
				}})
			}
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

			err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: true, PruneSessions: tc.prune, DryRun: tc.dryRun})

			require.ErrorIs(t, err, ErrLinkedWindow)
			require.ErrorContains(t, err, `window "shell" (@2) in session "dev"`)
			require.ErrorContains(t, err, "shared across sessions")
			require.Contains(t, appshared.ErrorHint(err), "No changes were applied.")
			require.Contains(t, appshared.ErrorHint(err), "Resolve the window links")
			require.Equal(t, 1, stub.queryCalls)
			require.Zero(t, stub.applyCalls, "even the planned tools window must not be created")
			require.Zero(t, stub.dryRunCalls)
			require.Zero(t, stub.attachCalls)
		})
	}
}

func TestForceRejectsUnsupportedObservedLayoutBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prune   bool
		dryRun  bool
		replace bool
		layout  string
	}{
		{name: "extra window"},
		{name: "extra window dry run", dryRun: true},
		{name: "extra window with pruning", prune: true},
		{name: "extra window with pruning dry run", prune: true, dryRun: true},
		{name: "mismatched window", replace: true},
		{name: "named preset pane mismatch", replace: true, layout: "tiled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, stub := pruningService()
			stub.queryResult.Sessions[0].Windows = append(stub.queryResult.Sessions[0].Windows, backend.Window{
				ID: "@3", Name: "scratch", Path: "/work", Layout: "opaque", Panes: []backend.Pane{{ID: "%3", Path: "/work"}},
			})
			unsupported := errors.New("unsupported observed layout")
			stub.validateObservedLayout = func(string) error { return unsupported }
			policy := &backendtmux.TmuxBackend{}
			stub.layoutMatch = policy.LayoutsMatch
			workspace := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{
				{Name: "editor", Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
				{Name: "tools", Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
			}}}}
			if tc.replace {
				workspace.Sessions[0].Windows = append(workspace.Sessions[0].Windows, manifest.Window{
					Name: "scratch", Path: "/work", Layout: tc.layout, Panes: []manifest.Pane{{Path: "/work"}, {Path: "/work"}},
				})
			}
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "/work/workspace.yaml", nil }

			var err error

			output := captureLoggerOutput(t, func() {
				err = service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: true, PruneSessions: tc.prune, DryRun: tc.dryRun})
			})

			require.ErrorIs(t, err, unsupported)
			require.Empty(t, output, "refusal must precede workspace and dry-run output")
			require.ErrorContains(t, err, `window "scratch" (@3) in session "dev"`)
			require.Contains(t, appshared.ErrorHint(err), "No changes were applied.")
			require.Equal(t, 1, stub.queryCalls)
			require.Zero(t, stub.applyCalls, "neither the planned tools window nor metadata may be applied")
			require.Zero(t, stub.dryRunCalls)
			require.Zero(t, stub.attachCalls)
		})
	}
}

func TestReconciliationValidatesOnlyRemovedWindowLayouts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		layout string
		force  bool
	}{
		{name: "valid legacy target with opaque retained windows", layout: "b25d,80x24,0,0,0", force: true},
		{name: "valid JSON target with opaque retained windows", layout: `{"V":2,"L":{"t":"p","w":80,"h":24,"x":0,"y":0,"i":0,"I":"%3"}}`, force: true},
		{name: "merge retains opaque extra window", layout: "opaque"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, stub := pruningService()
			stub.queryResult.Sessions[0].Windows[0].Layout = "opaque retained window"
			stub.queryResult.Sessions[1].Windows[0].Layout = "opaque unrelated window"
			stub.queryResult.Sessions[0].Windows = append(stub.queryResult.Sessions[0].Windows, backend.Window{
				ID: "@3", Name: "scratch", Path: "/work", Layout: tc.layout, Panes: []backend.Pane{{ID: "%3", Path: "/work"}},
			})
			policy := &backendtmux.TmuxBackend{}
			stub.validateObservedLayout = policy.ValidateObservedLayout

			err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: tc.force, Detach: true})

			require.NoError(t, err)
			require.Equal(t, 1, stub.queryCalls)
			require.Zero(t, stub.attachCalls)
			if tc.force {
				require.Equal(t, []backend.Action{backend.KillWindowAction{Session: "dev", Window: "scratch", WindowID: "@3"}}, stub.lastActions)
			} else {
				require.Zero(t, stub.applyCalls)
			}
		})
	}
}

func TestReconciliationAllowsWindowsWithoutSharedRemoval(t *testing.T) {
	for _, tc := range []struct {
		name       string
		otherID    string
		force      bool
		keepShared bool
		removedIDs []string
	}{
		{name: "same name but independent windows", otherID: "@20", force: true, removedIDs: []string{"@2", "@3"}},
		{name: "linked window retained while another is removed", otherID: "@2", force: true, keepShared: true, removedIDs: []string{"@3"}},
		{name: "merge leaves extra linked window", otherID: "@2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, stub := pruningService()
			shared := stub.queryResult.Sessions[1].Windows[0]
			stub.queryResult.Sessions[0].Windows = append(stub.queryResult.Sessions[0].Windows, shared, backend.Window{
				ID: "@3", Name: "scratch", Path: "/work", Panes: []backend.Pane{{ID: "%3", Path: "/work"}},
			})
			stub.queryResult.Sessions[1].Windows[0].ID = tc.otherID
			if tc.otherID != shared.ID {
				stub.queryResult.Sessions[1].Windows[0].Panes = []backend.Pane{{ID: "%20", Path: "/work"}}
			}
			stub.queryResult.Invocation = &backend.Invocation{}
			workspace := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{
				{Name: "editor", Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
				{Name: "tools", Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
			}}}}
			if tc.keepShared {
				workspace.Sessions[0].Windows = append(workspace.Sessions[0].Windows, manifest.Window{
					Name: shared.Name, Path: "/work", Panes: []manifest.Pane{{Path: "/work"}},
				})
			}
			service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }

			err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: tc.force, Detach: true})

			require.NoError(t, err)
			require.Equal(t, 1, stub.queryCalls)
			require.Equal(t, 1, stub.applyCalls)
			require.Zero(t, stub.attachCalls)
			var removedIDs []string
			for _, action := range stub.lastActions {
				if removal, ok := action.(backend.KillWindowAction); ok {
					removedIDs = append(removedIDs, removal.WindowID)
				}
			}
			require.ElementsMatch(t, tc.removedIDs, removedIDs)
		})
	}
}

func TestPruningProtectsTheInvokingSession(t *testing.T) {
	for _, tc := range []struct {
		name       string
		invocation *backend.Invocation
		wantError  bool
	}{
		{"omitted popup session", &backend.Invocation{SessionID: "$2"}, true},
		{"pane moved from retained session", &backend.Invocation{SessionID: "$1", PaneID: "%2"}, true},
		{"pane moved into retained session", &backend.Invocation{SessionID: "$2", PaneID: "%1"}, false},
		{"unknown invocation", &backend.Invocation{}, true},
		{"missing invoking pane", &backend.Invocation{SessionID: "$1", PaneID: "%99"}, true},
		{"retained invoking session", &backend.Invocation{SessionID: "$1", PaneID: "%1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, stub := pruningService()
			stub.queryResult.Invocation = tc.invocation

			err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), PruneSessions: true, DryRun: true})

			if tc.wantError {
				require.ErrorIs(t, err, ErrUnsafePruning)
				require.Zero(t, stub.dryRunCalls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, stub.dryRunCalls)
			}
			require.Zero(t, stub.applyCalls)
			require.Zero(t, stub.attachCalls)
		})
	}
}

func TestPruningProtectsTheInvokingWindow(t *testing.T) {
	for _, tc := range []struct {
		name      string
		paneID    string
		wantError bool
	}{
		{"invoking window removed", "%3", true},
		{"other window removed", "%1", false},
		{"popup window unknown", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, stub := pruningService()
			stub.queryResult.Invocation = &backend.Invocation{SessionID: "$1", PaneID: tc.paneID}
			stub.queryResult.Sessions[0].Windows = append(stub.queryResult.Sessions[0].Windows, backend.Window{
				ID: "@3", Name: "scratch", Path: "/work", Panes: []backend.Pane{{ID: "%3", Path: "/work"}},
			})

			err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: true, PruneSessions: true})

			if tc.wantError {
				require.ErrorIs(t, err, ErrUnsafePruning)
				require.Zero(t, stub.applyCalls)
				require.Zero(t, stub.attachCalls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, stub.applyCalls)
			}
		})
	}
}

func pruningService() (Service, *stubBackend) {
	workspace := &manifest.Workspace{Sessions: []manifest.Session{{Name: "dev", Windows: []manifest.Window{
		{Name: "editor", Path: "/work", Panes: []manifest.Pane{{Path: "/work"}}},
	}}}}
	stub := &stubBackend{queryResult: backend.StateResult{Sessions: []backend.Session{
		{ID: "$1", Name: "dev", Windows: []backend.Window{
			{ID: "@1", Name: "editor", Path: "/work", Panes: []backend.Pane{{ID: "%1", Path: "/work"}}},
		}},
		{ID: "$2", Name: "old", Windows: []backend.Window{
			{ID: "@2", Name: "shell", Path: "/work", Panes: []backend.Pane{{ID: "%2", Path: "/work"}}},
		}},
	}}}
	service := NewService(func() (Backend, error) { return stub, nil }, acceptLayout)
	service.LoadWorkspace = func(string) (*manifest.Workspace, string, error) { return workspace, "", nil }
	return service, stub
}
