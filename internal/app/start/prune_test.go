package start

import (
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/stretchr/testify/require"
)

func TestPruningRejectsSharedWindowDestructionBeforeMutation(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "dry run"}[dryRun], func(t *testing.T) {
			service, stub := pruningService()
			shared := stub.queryResult.Sessions[1].Windows[0]
			stub.queryResult.Sessions[0].Windows = append(stub.queryResult.Sessions[0].Windows, shared)

			err := service.Run(t.Context(), Options{Path: new("workspace.yaml"), Force: true, PruneSessions: true, DryRun: dryRun})

			require.ErrorIs(t, err, ErrUnsafePruning)
			require.Zero(t, stub.applyCalls)
			require.Zero(t, stub.dryRunCalls)
			require.Zero(t, stub.attachCalls)
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
