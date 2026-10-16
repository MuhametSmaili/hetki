package tmux

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLastSessionMarkerUsesOneStateBatch(t *testing.T) {
	t.Setenv("TMUX", ",,1")
	t.Setenv("TMUX_PANE", "%1")
	calls := 0
	output := "0\n0\n" +
		"$1|current|@1|main|0|layout|0|1|%1|0|1|/current|sh||100|0|/bin/sh\n" +
		"$2|last\\|name|@2|editor|0|layout|0|1|%2|0|1|/last|sh||101|0|/bin/sh\n" +
		"client|$1|%1|last\\|name|0\nclient|$2|%2|current|0\nclient|$1|%1||1\n"
	b := &TmuxBackend{client: &MockClient{RunFunc: func(_ context.Context, args ...string) (string, error) {
		calls++
		require.Contains(t, args, "list-panes")
		require.Contains(t, args, "list-clients")
		return output, nil
	}}}
	state, err := b.QueryState(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.False(t, state.Sessions[0].Last)
	require.True(t, state.Sessions[1].Last)
	output = strings.ReplaceAll(output, "last\\|name", "renamed")
	state, err = b.QueryState(context.Background())
	require.NoError(t, err)
	require.Equal(t, "$2", state.Sessions[1].ID)
	require.True(t, state.Sessions[1].Last)
}

func TestInvokingClientDoesNotGuessAcrossContexts(t *testing.T) {
	owner := clientState{sessionID: "$1", paneID: "%1", lastSession: "last"}
	other := clientState{sessionID: "$2", paneID: "%2", lastSession: "current"}
	observer := clientState{sessionID: "$1", paneID: "%1", observer: true}
	for _, test := range []struct {
		name, tmux, pane string
		clients          []clientState
		want             clientState
	}{
		{name: "shell", tmux: ",,1", pane: "%1", clients: []clientState{owner, other}, want: owner},
		{name: "popup", tmux: ",,1", clients: []clientState{owner, other}, want: owner},
		{name: "shell with observer", tmux: ",,1", pane: "%1", clients: []clientState{owner, observer}, want: owner},
		{name: "popup with observer", tmux: ",,1", clients: []clientState{observer, owner}, want: owner},
		{name: "observer without owner", tmux: ",,1", clients: []clientState{observer}},
		{name: "outside tmux", pane: "%1", clients: []clientState{owner}},
		{name: "missing client", tmux: ",,1", pane: "%1", clients: []clientState{other}},
		{name: "ambiguous shell", tmux: ",,1", pane: "%1", clients: []clientState{owner, owner}},
		{name: "ambiguous popup", tmux: ",,1", clients: []clientState{owner, owner}},
		{name: "invalid pane", tmux: ",,1", pane: "bad", clients: []clientState{owner}},
		{name: "invalid session", tmux: "bad", clients: []clientState{owner}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TMUX", test.tmux)
			t.Setenv("TMUX_PANE", test.pane)
			require.Equal(t, test.want, invokingClient(test.clients))
		})
	}
}

func TestClientStateParsingRejectsMalformedRecords(t *testing.T) {
	for _, row := range []string{
		"client|name|%1|last|0", "client|$1|pane|last|0", "client|$01|%1|last|0",
		"client|$1|%1|last", "client|$1|%1|last|maybe", "client|$1|%1|last\\", "client|$1|%1|last\nextra|0",
	} {
		_, err := (LoadStateQuery{IncludeClients: true}).Parse("0\n0\n" + row + "\n")
		require.Error(t, err, row)
	}
}
