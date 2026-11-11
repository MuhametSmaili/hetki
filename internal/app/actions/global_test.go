package actions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func globalCatalog(steps ...Step) Catalog {
	return Catalog{Version: 1, Actions: map[string]Recipe{"global": {Label: "Global", Scope: []string{"global"}, Steps: steps}}}
}

func TestGlobalFlowOmitsAbsentContext(t *testing.T) {
	for _, socket := range []string{"", "/fixture/not-running"} {
		t.Run("socket="+socket, func(t *testing.T) {
			dir := t.TempDir()
			providerInput, finalInput := filepath.Join(dir, "provider.json"), filepath.Join(dir, "final.json")
			inventory := &inventoryStub{}
			inventory.server.Socket = socket
			catalog := globalCatalog(
				Step{Select: "fixed", Source: &SourceSpec{Items: []Item{{ID: "fixed", Label: "Fixed"}}}},
				Step{Select: "external", Source: &SourceSpec{Exec: &Command{Argv: []string{"sh", "-c", `cat > "$1"; printf '%s\n' '{"id":"external","label":"External"}'`, "provider", providerInput}}}},
				Step{Run: captureCommand(finalInput)},
			)
			flow, err := New(t.Context(), catalog, inventory, Subject{})
			require.NoError(t, err)

			_, err = flow.Select(t.Context(), "global")
			require.NoError(t, err)
			_, err = flow.Select(t.Context(), "fixed")
			require.NoError(t, err)
			// A server that starts later must not change the initial context.
			inventory.server = flowInventory().server
			screen, err := flow.Select(t.Context(), "external")

			require.NoError(t, err)
			assert.True(t, screen.Done)
			for _, path := range []string{providerInput, finalInput} {
				data, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				var request struct {
					Context map[string]any `json:"context"`
				}
				require.NoError(t, json.Unmarshal(data, &request))
				assert.NotContains(t, request.Context, "subject")
				assert.NotContains(t, request.Context, "invocation")
				if socket == "" {
					assert.NotContains(t, request.Context, "server")
				} else {
					assert.Equal(t, map[string]any{"socket": socket}, request.Context["server"])
				}
			}
		})
	}
}

func TestFlowInferredAndExplicitSubjectEligibility(t *testing.T) {
	for _, test := range []struct {
		name       string
		invocation *backend.Invocation
		subject    Subject
		wantKind   string
		wantPane   string
		wantError  bool
	}{
		{name: "outside tmux"},
		{name: "inferred pane", invocation: &backend.Invocation{SessionID: "$1", PaneID: "%1"}, wantKind: "pane", wantPane: "%1"},
		{name: "ambiguous invocation", invocation: &backend.Invocation{}, wantError: true},
		{name: "missing inferred pane", invocation: &backend.Invocation{SessionID: "$1", PaneID: "%99"}, wantError: true},
		{name: "explicit override", invocation: &backend.Invocation{SessionID: "$1", PaneID: "%1"}, subject: Subject{Kind: "pane", PaneID: "%2"}, wantKind: "pane", wantPane: "%2"},
		{name: "explicit window overrides ambiguity", invocation: &backend.Invocation{}, subject: Subject{Kind: "window", WindowID: "@1"}, wantKind: "window"},
		{name: "explicit session", subject: Subject{Kind: "session", SessionID: "$1"}, wantKind: "session"},
		{name: "missing explicit subject", subject: Subject{Kind: "pane", PaneID: "%99"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "request.json")
			catalog := globalCatalog(Step{Run: captureCommand(output)})
			for _, scope := range []string{"pane", "window", "session"} {
				catalog.Actions[scope] = Recipe{Label: scope, Scope: []string{scope}, Steps: []Step{{Run: captureCommand(output)}}}
			}
			inventory := flowInventory()
			inventory.state.Invocation = test.invocation

			flow, err := New(t.Context(), catalog, inventory, test.subject)

			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			want := []string{"global"}
			if test.wantKind != "" {
				want = append(want, test.wantKind)
			}
			var ids []string
			for _, item := range flow.Screen().Items {
				ids = append(ids, item.ID)
			}
			assert.ElementsMatch(t, want, ids)
			_, err = flow.Select(t.Context(), "global")
			require.NoError(t, err)
			data, err := os.ReadFile(output)
			require.NoError(t, err)
			var request struct {
				Context struct {
					Subject Subject `json:"subject"`
				} `json:"context"`
			}
			require.NoError(t, json.Unmarshal(data, &request))
			assert.Equal(t, test.wantKind, request.Context.Subject.Kind)
			assert.Equal(t, test.wantPane, request.Context.Subject.PaneID)
		})
	}
}

func TestEmptyActionMenuExplainsScopeAndCanClose(t *testing.T) {
	for _, test := range []struct {
		name    string
		subject Subject
		scope   string
		want    string
	}{
		{name: "no subject", scope: "pane", want: "no global actions configured"},
		{name: "pane", subject: Subject{Kind: "pane", PaneID: "%1"}, scope: "session", want: "no actions for this pane"},
		{name: "window", subject: Subject{Kind: "window", WindowID: "@1"}, scope: "pane", want: "no actions for this window"},
		{name: "session", subject: Subject{Kind: "session", SessionID: "$1"}, scope: "pane", want: "no actions for this session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := Catalog{Version: 1, Actions: map[string]Recipe{
				"other": {Label: "Other scope", Scope: []string{test.scope}, Steps: []Step{{Run: &Command{Argv: []string{"true"}}}}},
			}}

			flow, err := New(t.Context(), catalog, flowInventory(), test.subject)

			require.NoError(t, err)
			screen := flow.Screen()
			assert.Empty(t, screen.Items)
			assert.False(t, screen.Done)
			assert.Equal(t, test.want, screen.EmptyText)
			screen, err = flow.Back(t.Context())
			require.NoError(t, err)
			assert.True(t, screen.Done)
		})
	}
}

func TestFlowInferredPaneOmitsLinkedSession(t *testing.T) {
	output := filepath.Join(t.TempDir(), "request.json")
	inventory := flowInventory()
	inventory.state.Invocation = &backend.Invocation{SessionID: "$1", PaneID: "%1"}
	inventory.state.Sessions = append(inventory.state.Sessions, backend.Session{
		ID: "$2", Windows: inventory.state.Sessions[0].Windows[:1],
	})
	catalog := flowCatalog(Step{Run: captureCommand(output)})

	flow, err := New(t.Context(), catalog, inventory, Subject{})
	require.NoError(t, err)
	screen, err := flow.Select(t.Context(), "move")

	require.NoError(t, err)
	assert.True(t, screen.Done)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	var request Request
	require.NoError(t, json.Unmarshal(data, &request))
	assert.Equal(t, &Subject{Kind: "pane", PaneID: "%1", WindowID: "@1"}, request.Context.Subject)
}

func TestGlobalFlowRevalidatesCapturedContext(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*inventoryStub)
	}{
		{"removed subject", func(s *inventoryStub) { s.state.Sessions[0].Windows = s.state.Sessions[0].Windows[1:] }},
		{"replaced server", func(s *inventoryStub) { s.server.Birth = "replacement" }},
		{"absent server", func(s *inventoryStub) { s.server.PID = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "mutation")
			inventory := flowInventory()
			flow, err := New(t.Context(), globalCatalog(Step{Run: captureCommand(output)}), inventory, Subject{Kind: "pane", PaneID: "%1"})
			require.NoError(t, err)
			test.change(inventory)

			_, err = flow.Select(t.Context(), "global")

			require.Error(t, err)
			_, err = os.Stat(output)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestServerlessFlowRejectsNativeSourcesAndMissingReferences(t *testing.T) {
	for _, test := range []struct {
		name string
		step Step
	}{
		{"native source", Step{Select: "session", Source: &SourceSpec{Builtin: "tmux.sessions"}}},
		{"subject reference", Step{Select: "external", Source: &SourceSpec{Exec: &Command{Argv: []string{"echo", "${context.subject.pane_id}"}}}}},
		{"socket reference", Step{Select: "external", Source: &SourceSpec{Exec: &Command{Argv: []string{"echo", "${context.server.socket}"}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			flow, err := New(t.Context(), globalCatalog(test.step, Step{Run: &Command{Argv: []string{"true"}}}), &inventoryStub{}, Subject{})
			require.NoError(t, err)

			screen, err := flow.Select(t.Context(), "global")

			require.Error(t, err)
			assert.Empty(t, screen.Items)
			assert.False(t, screen.Done)
		})
	}
}
