package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestSessionPromptUsesSelectedDirectoryOrHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, test := range []struct {
		name   string
		view   ui.ViewMode
		itemID list.ItemID
		path   string
		want   string
	}{
		{name: "session active window and pane", view: ui.ViewTree, itemID: sessionItemID("$2"), path: "/selected", want: "/selected"},
		{name: "window active pane", view: ui.ViewTree, itemID: windowItemID("$2", "@2"), path: "/selected", want: "/selected"},
		{name: "inactive window", view: ui.ViewTree, itemID: windowItemID("$2", "@3"), want: "/other-window"},
		{name: "exact tree pane", view: ui.ViewTree, itemID: destinationItemID("$2", "@2", "%3"), want: "/other-pane"},
		{name: "exact flat pane", view: ui.ViewFlat, itemID: destinationItemID("$2", "@2", "%3"), want: "/other-pane"},
		{name: "session without a path", view: ui.ViewTree, itemID: sessionItemID("$2"), want: home},
		{name: "window without a path", view: ui.ViewTree, itemID: windowItemID("$2", "@2"), want: home},
		{name: "pane without a path", view: ui.ViewFlat, itemID: destinationItemID("$2", "@2", "%2"), want: home},
		{name: "no selection", view: ui.ViewTree, want: home},
		{name: "stale selection", view: ui.ViewTree, itemID: sessionItemID("$99"), want: home},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &stubBackend{state: liveState()}
			stub.state.Invocation = &backend.Invocation{SessionID: "$1", PaneID: "%1"}
			stub.state.Sessions = append(stub.state.Sessions, backend.Session{
				ID:   "$2",
				Name: "selected",
				Windows: []backend.Window{
					{
						ID: "@3", Index: 0, Name: "other",
						Panes: []backend.Pane{{ID: "%4", Path: "/other-window", Active: true}},
					},
					{
						ID: "@2", Index: 1, Name: "active", Active: true,
						Panes: []backend.Pane{
							{ID: "%3", Index: 0, Path: "/other-pane"},
							{ID: "%2", Index: 1, Path: test.path, Active: true},
						},
					},
				},
			})
			adapter := testAdapter(stub)
			_, err := adapter.Load(t.Context(), test.view)
			require.NoError(t, err)

			result, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionCreateSession, ItemID: test.itemID})

			require.NoError(t, err)
			require.Len(t, result.Input.Fields, 2)
			require.Equal(t, ui.InputField{
				ID: sessionNameField, Label: "Session name", Kind: ui.InputText,
				Placeholder: "Type session name…", Required: true,
			}, result.Input.Fields[0])
			require.Equal(t, test.want, result.Input.Fields[1].InitialValue)
			require.Equal(t, test.want, result.Input.Fields[1].BasePath)
			require.Equal(t, sessionDirectoryField, result.Input.Fields[1].ID)
			require.Equal(t, ui.InputDirectory, result.Input.Fields[1].Kind)
			require.Equal(t, 1, stub.queryCalls, "opening the form reuses the picker inventory")
			require.Empty(t, stub.applyCalls)
			require.Nil(t, adapter.pendingNavigation, "prefilling a path must not prepare a switch")
		})
	}
}

func TestSessionPromptReportsUnavailableHome(t *testing.T) {
	t.Setenv("HOME", "")
	adapter := loadedAdapter(t, &stubBackend{state: liveState()})

	_, err := adapter.Execute(t.Context(), ui.ActionRequest{ActionID: ui.ActionCreateSession})

	require.ErrorContains(t, err, "find home directory")
}

func TestCreateSessionUsesValidatedDirectoryAndPreservesWorkspace(t *testing.T) {
	root := t.TempDir()
	stub := &stubBackend{state: liveState()}
	stub.applyHook = func([]backend.Action) {
		stub.state.Sessions = append(stub.state.Sessions, backend.Session{ID: "$2", Name: "sandbox"})
	}
	adapter := loadedAdapter(t, stub)

	result, err := adapter.Execute(t.Context(), ui.ActionRequest{
		ActionID: ui.ActionCreateSession, Values: map[string]string{sessionNameField: "sandbox", sessionDirectoryField: root},
	})

	require.NoError(t, err)
	require.Equal(t, [][]backend.Action{{
		backend.CreateSessionAction{Name: "sandbox", Path: root},
		backend.SetSessionOptionAction{Session: "sandbox", Key: backend.WorkspacePathOption, Value: "/work/.hetki.yaml"},
	}}, stub.applyCalls)
	require.Equal(t, sessionItemID("$2"), result.SelectItemID)
	require.Empty(t, result.Navigation)
}

func TestCreateSessionRejectsInvalidInputsBeforeMutation(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	for _, test := range []struct {
		name, session, path string
	}{
		{name: "missing path", session: "api", path: filepath.Join(root, "missing")},
		{name: "file", session: "api", path: file},
		{name: "empty directory", session: "api"},
		{name: "empty name", path: root},
		{name: "whitespace name", session: "  ", path: root},
		{name: "colon in name", session: "api:work", path: root},
		{name: "dot in name", session: "api.work", path: root},
		{name: "control in name", session: "api\x00work", path: root},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &stubBackend{state: liveState()}
			adapter := loadedAdapter(t, stub)

			_, err := adapter.Execute(t.Context(), ui.ActionRequest{
				ActionID: ui.ActionCreateSession, Values: map[string]string{sessionNameField: test.session, sessionDirectoryField: test.path},
			})

			require.Error(t, err)
			require.Empty(t, stub.applyCalls)
		})
	}
}

func TestSessionSubmissionRequiresExplicitNameInsteadOfDirectoryBasename(t *testing.T) {
	stub := &stubBackend{state: liveState()}
	adapter := loadedAdapter(t, stub)

	_, err := adapter.Execute(t.Context(), ui.ActionRequest{
		ActionID: ui.ActionCreateSession, Values: map[string]string{sessionDirectoryField: t.TempDir()},
	})

	require.ErrorContains(t, err, "session name is required")
	require.Empty(t, stub.applyCalls)
}
