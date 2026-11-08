package actions

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inventoryStub struct {
	state  backend.StateResult
	server observe.Server
}

func (s *inventoryStub) QueryStatusState(ctx context.Context) (backend.StateResult, observe.Server, error) {
	return s.state, s.server, ctx.Err()
}

func flowInventory() *inventoryStub {
	return &inventoryStub{
		server: observe.Server{Socket: "/fixture/tmux", PID: 1, Started: 2, Birth: "fixture"},
		state: backend.StateResult{Sessions: []backend.Session{{ID: "$1", Name: "work", Windows: []backend.Window{
			{ID: "@1", Name: "editor", Panes: []backend.Pane{{ID: "%1", Active: true}}},
			{ID: "@2", Name: "editor", Panes: []backend.Pane{{ID: "%2", Active: true}}},
		}}}},
	}
}

func flowCatalog(steps ...Step) Catalog {
	return Catalog{Version: 1, Actions: map[string]Recipe{"move": {Label: "Move pane", Scope: []string{"pane"}, Steps: steps}}}
}

func captureCommand(path string) *Command {
	return &Command{Argv: []string{"sh", "-c", `cat > "$1"`, "capture", path}}
}

func TestFlowPreservesSubjectAndPassesNamedSelections(t *testing.T) {
	output := filepath.Join(t.TempDir(), "request.json")
	catalog := flowCatalog(
		Step{Select: "destination", Source: &SourceSpec{Builtin: "tmux.windows"}},
		Step{Run: captureCommand(output)},
	)
	f, err := New(t.Context(), catalog, flowInventory(), Subject{Kind: "pane", PaneID: "%1"})
	require.NoError(t, err)

	screen, err := f.Select(t.Context(), "move")
	require.NoError(t, err)
	require.Len(t, screen.Items, 2)
	assert.Equal(t, screen.Items[0].Label, screen.Items[1].Label)
	screen, err = f.Select(t.Context(), "$1:@2")
	require.NoError(t, err)

	assert.True(t, screen.Done)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	var request Request
	require.NoError(t, json.Unmarshal(data, &request))
	assert.Equal(t, "%1", request.Context.Subject.PaneID)
	assert.Equal(t, "/fixture/tmux", request.Context.Server.Socket)
	assert.Equal(t, "$1:@2.%2", request.Steps["destination"].Value["pane_target"])
	_, err = f.Select(t.Context(), "$1:@2")
	require.Error(t, err)
}

func TestFlowBackReselectionClearsDependentValues(t *testing.T) {
	output := filepath.Join(t.TempDir(), "request.json")
	catalog := flowCatalog(
		Step{Select: "session", Source: &SourceSpec{Builtin: "tmux.sessions"}},
		Step{Select: "window", Source: &SourceSpec{Builtin: "tmux.windows", Session: "${steps.session.value.session_id}"}},
		Step{Run: captureCommand(output)},
	)
	f, err := New(t.Context(), catalog, flowInventory(), Subject{Kind: "pane", PaneID: "%1"})
	require.NoError(t, err)
	_, err = f.Select(t.Context(), "move")
	require.NoError(t, err)
	_, err = f.Select(t.Context(), "$1")
	require.NoError(t, err)

	screen, err := f.Back(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "move/0", screen.Key)
	assert.Empty(t, f.request.Steps)
	_, err = f.Select(t.Context(), "$1")
	require.NoError(t, err)
	_, err = f.Select(t.Context(), "$1:@2")
	require.NoError(t, err)

	data, err := os.ReadFile(output)
	require.NoError(t, err)
	var request Request
	require.NoError(t, json.Unmarshal(data, &request))
	assert.Len(t, request.Steps, 2)
	assert.Equal(t, "$1:@2", request.Steps["window"].ID)
}

func TestFailedCommandRefreshAndBackDoNotReplay(t *testing.T) {
	output := filepath.Join(t.TempDir(), "attempts")
	catalog := flowCatalog(Step{Run: &Command{Argv: []string{"sh", "-c", `printf x >> "$1"; exit 7`, "test", output}}})
	f, err := New(t.Context(), catalog, flowInventory(), Subject{Kind: "pane", PaneID: "%1"})
	require.NoError(t, err)

	screen, err := f.Select(t.Context(), "move")
	require.Error(t, err)
	assert.False(t, screen.Done)
	screen, err = f.Refresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "run", screen.Items[0].ID)
	_, err = f.Back(t.Context())
	require.NoError(t, err)

	data, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "x", string(data))

	_, err = f.Select(t.Context(), "move")
	require.Error(t, err)
	_, err = f.Select(t.Context(), "run")
	require.Error(t, err)

	data, err = os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "xxx", string(data), "only explicit selections retry a failed command")
}

func TestSuccessfulCommandWithIncompleteCaptureCannotReplay(t *testing.T) {
	for _, closeWith := range []string{"select", "back"} {
		t.Run(closeWith, func(t *testing.T) {
			command, attempts := orphanOutputCommand(t)
			f, err := New(t.Context(), flowCatalog(Step{Run: &command}), flowInventory(), Subject{Kind: "pane", PaneID: "%1"})
			require.NoError(t, err)

			screen, err := f.Select(t.Context(), "move")

			require.ErrorIs(t, err, exec.ErrWaitDelay)
			require.ErrorContains(t, err, "exited successfully")
			require.Len(t, screen.Items, 1)
			assert.Equal(t, "done", screen.Items[0].ID)
			assert.False(t, screen.Done)
			_, err = f.Select(t.Context(), "run")
			require.Error(t, err)
			refreshed, err := f.Refresh(t.Context())
			require.NoError(t, err)
			assert.Equal(t, screen, refreshed)
			if closeWith == "select" {
				screen, err = f.Select(t.Context(), "done")
			} else {
				screen, err = f.Back(t.Context())
			}
			require.NoError(t, err)
			assert.True(t, screen.Done)
			_, err = f.Select(t.Context(), "move")
			require.Error(t, err)
			data, err := os.ReadFile(attempts)
			require.NoError(t, err)
			assert.Equal(t, "x", string(data))
		})
	}
}

func TestProviderRejectsSuccessfulExitWithIncompleteCapture(t *testing.T) {
	command, _ := orphanOutputCommand(t)
	catalog := flowCatalog(
		Step{Select: "target", Source: &SourceSpec{Exec: &command}},
		Step{Run: &Command{Argv: []string{"true"}}},
	)
	f, err := New(t.Context(), catalog, flowInventory(), Subject{Kind: "pane", PaneID: "%1"})
	require.NoError(t, err)

	screen, err := f.Select(t.Context(), "move")

	require.ErrorIs(t, err, exec.ErrWaitDelay)
	assert.Empty(t, screen.Items)
	assert.False(t, screen.Done)
}

func orphanOutputCommand(t *testing.T) (Command, string) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
	attempts := filepath.Join(t.TempDir(), "attempts")
	return helperCommand(t, "orphan-output", listener.Addr().String(), attempts), attempts
}

func TestFlowRefusesRemovedTargetAndReplacementServer(t *testing.T) {
	for _, scenario := range []string{"removed target", "restarted server"} {
		t.Run(scenario, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "mutation")
			inventory := flowInventory()
			catalog := flowCatalog(
				Step{Select: "destination", Source: &SourceSpec{Builtin: "tmux.windows"}},
				Step{Run: captureCommand(output)},
			)
			f, err := New(t.Context(), catalog, inventory, Subject{Kind: "pane", PaneID: "%1"})
			require.NoError(t, err)
			_, err = f.Select(t.Context(), "move")
			require.NoError(t, err)
			if scenario == "removed target" {
				inventory.state.Sessions[0].Windows = inventory.state.Sessions[0].Windows[:1]
			} else {
				inventory.server.Birth = "replacement"
			}

			_, err = f.Select(t.Context(), "$1:@2")

			require.Error(t, err)
			_, statErr := os.Stat(output)
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestProviderFailurePublishesNoPreviousChoices(t *testing.T) {
	catalog := flowCatalog(
		Step{Select: "broken", Source: &SourceSpec{Exec: &Command{Argv: []string{"sh", "-c", `printf '{"id":"partial","label":"Partial"}\n'; exit 1`}}}},
		Step{Run: &Command{Argv: []string{"true"}}},
	)
	f, err := New(t.Context(), catalog, flowInventory(), Subject{Kind: "pane", PaneID: "%1"})
	require.NoError(t, err)

	screen, err := f.Select(t.Context(), "move")

	require.Error(t, err)
	assert.Equal(t, "move/0", screen.Key)
	assert.Empty(t, screen.Items)
	_, err = f.Select(t.Context(), "move")
	require.Error(t, err)
}

func TestLinkedPaneDoesNotInventAnOwningSession(t *testing.T) {
	inventory := flowInventory()
	linked := inventory.state.Sessions[0]
	linked.ID = "$2"
	inventory.state.Sessions = append(inventory.state.Sessions, linked)
	catalog := flowCatalog(Step{Run: &Command{Argv: []string{"true"}}})

	f, err := New(t.Context(), catalog, inventory, Subject{Kind: "pane", PaneID: "%1"})

	require.NoError(t, err)
	assert.Empty(t, f.request.Context.Subject.SessionID)
	assert.Equal(t, "%1", f.request.Context.Subject.PaneID)
	_, err = f.request.reference("context.subject.session_id")
	require.Error(t, err)
}

func TestExternalSourceReceivesPriorSelectionAndReturnsOpaqueValues(t *testing.T) {
	dir := t.TempDir()
	providerInput, finalInput := filepath.Join(dir, "provider.json"), filepath.Join(dir, "final.json")
	provider := &Command{Argv: []string{"sh", "-c", `cat > "$1"; printf '%s\n' '{"id":"opaque","label":"Custom choice","value":{"path":"two words; literal"}}'`, "provider", providerInput}}
	catalog := flowCatalog(
		Step{Select: "session", Source: &SourceSpec{Builtin: "tmux.sessions"}},
		Step{Select: "custom", Source: &SourceSpec{Exec: provider}},
		Step{Run: captureCommand(finalInput)},
	)
	f, err := New(t.Context(), catalog, flowInventory(), Subject{Kind: "pane", PaneID: "%1"})
	require.NoError(t, err)
	_, err = f.Select(t.Context(), "move")
	require.NoError(t, err)

	screen, err := f.Select(t.Context(), "$1")
	require.NoError(t, err)
	require.Len(t, screen.Items, 1)
	_, err = f.Select(t.Context(), "opaque")
	require.NoError(t, err)

	data, err := os.ReadFile(providerInput)
	require.NoError(t, err)
	var request Request
	require.NoError(t, json.Unmarshal(data, &request))
	assert.Equal(t, "%1", request.Context.Subject.PaneID)
	assert.Equal(t, "$1", request.Steps["session"].ID)
	data, err = os.ReadFile(finalInput)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &request))
	assert.Equal(t, "two words; literal", request.Steps["custom"].Value["path"])
}
