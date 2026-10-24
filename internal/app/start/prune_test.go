package start

import (
	"slices"
	"testing"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
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
