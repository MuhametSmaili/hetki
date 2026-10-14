package tui

import (
	"context"
	"testing"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/frecency"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestProjectionKeepsOpaqueOccurrenceIDsDistinct(t *testing.T) {
	state := backend.StateResult{Sessions: []backend.Session{
		{ID: "session:one", Name: "first", Windows: []backend.Window{{
			ID: "window", Panes: []backend.Pane{{ID: `pane\:one`}},
		}}},
		{ID: "session", Name: "second", Windows: []backend.Window{{
			ID: "one:window", Panes: []backend.Pane{{ID: `pane\:one`}},
		}}},
		{ID: `session\`, Name: "third", Windows: []backend.Window{{
			ID: "one:window", Panes: []backend.Pane{{ID: "pane"}},
		}}},
		{ID: `session:one\`, Name: "fourth", Windows: []backend.Window{{
			ID: "window", Panes: []backend.Pane{{ID: "pane"}},
		}}},
	}}

	for _, test := range []struct {
		name string
		kind projectionKind
	}{
		{name: "tree", kind: projectionTree},
		{name: "flat", kind: projectionFlat},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, index, err := projectState(state, test.kind, frecency.Scores{}, "")

			require.NoError(t, err)
			require.NoError(t, validateProjection(snapshot, index))
			require.Len(t, snapshot.Items, 4)
			require.NotEqual(t, windowItemID("session:one", "window"), windowItemID("session", "one:window"))
			require.NotEqual(t, destinationItemID("session:one", "window", `pane\:one`), destinationItemID("session", "one:window", `pane\:one`))
		})
	}
}

func TestLiveAdapterUsesOpaqueReferenceForPaneActions(t *testing.T) {
	target := backend.Target{SessionID: " session:one ", WindowID: "window.with:punctuation", PaneID: "pane"}
	stub := &stubBackend{state: backend.StateResult{Sessions: []backend.Session{{
		ID: target.SessionID, Name: "session", Windows: []backend.Window{{
			ID: target.WindowID, Name: "window", Panes: []backend.Pane{{ID: target.PaneID}},
		}},
	}}}}
	stub.captureHook = func(_ context.Context, captured backend.Target) (string, error) {
		require.Equal(t, target, captured)
		return "screen", nil
	}
	adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, nil, nil)
	snapshot, err := adapter.Load(context.Background())
	require.NoError(t, err)
	id := snapshot.Items[0].ID

	opened, err := adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionOpen, ItemID: id})
	require.NoError(t, err)
	require.Equal(t, id, opened.Navigation)
	require.NoError(t, adapter.Navigate(context.Background(), opened.Navigation))
	require.Equal(t, []backend.Target{target}, stub.switchCalls)
	screen, err := adapter.Preview(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "screen", screen)

	_, err = adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionRename, ItemID: id, Value: text("renamed")})
	require.NoError(t, err)
	_, err = adapter.Execute(context.Background(), ui.ActionRequest{ActionID: ui.ActionDelete, ItemID: id, Confirmed: true})

	require.NoError(t, err)
	require.Equal(t, [][]backend.Action{
		{backend.RenameWindowAction{Session: target.SessionID, Window: target.WindowID, WindowID: target.WindowID, New: "renamed"}},
		{backend.KillWindowAction{Session: target.SessionID, Window: target.WindowID, WindowID: target.WindowID}},
	}, stub.applyCalls)
}
