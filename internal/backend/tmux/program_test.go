package tmux

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProgramLookupFallsBackWithoutChangingRawCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lookup := newProgramLookup(ctx)
	for _, pane := range []Pane{
		{Command: "node", PID: 123},
		{Command: "node", PID: 123, Dead: true},
		{Command: "node"},
		{Command: "nvim", PID: 123},
	} {
		require.Empty(t, lookup.resolve(pane))
	}
	lookup = newProgramLookup(context.Background())
	lookup.deadline = time.Now().Add(-time.Second)
	require.Empty(t, lookup.resolve(Pane{Command: "node", PID: 123}))
	lookup.deadline = time.Now().Add(time.Second)
	lookup.remaining = 0
	require.Empty(t, lookup.resolve(Pane{Command: "node", PID: 123}))
	lookup.remaining = 1
	lookup.cache[123] = "unlisted-cli"
	require.Equal(t, "unlisted-cli", lookup.resolve(Pane{Command: "node", PID: 123}))
	require.Equal(t, 1, lookup.remaining)
}

func TestLoadStateQueryProgramInfoIsOptionalAndStrict(t *testing.T) {
	q := LoadStateQuery{IncludeProgramInfo: true}
	require.Contains(t, q.Args()[len(q.Args())-1], "|#{pane_pid}|#{pane_dead}|#{q:default-shell}")
	row := "0\n0\n$1|dev|@1|main|0||0|1|%1|0|1|/work|node||123|1|/bin/sh"
	state, err := q.Parse(row)
	require.NoError(t, err)
	pane := state.Sessions[0].Windows[0].Panes[0]
	require.Equal(t, 123, pane.PID)
	require.True(t, pane.Dead)
	require.Equal(t, "node", pane.Command)
	for _, suffix := range []string{"", "|123", "|123|0", "|0|0|/bin/sh", "|-1|0|/bin/sh", "|123|2|/bin/sh", "|bad|0|/bin/sh", "|123|0|/bin/sh|extra"} {
		_, err := q.Parse("0\n0\n$1|dev|@1|main|0||0|1|%1|0|1|/work|node|" + suffix)
		require.Error(t, err, suffix)
	}
}

func TestProgramLookupCapsNativeAttemptsPerSnapshot(t *testing.T) {
	lookup := newProgramLookup(context.Background())
	lookup.deadline = time.Now().Add(time.Hour)
	for i := range maxProgramPanes + 10 {
		// Unallocated PIDs keep the test away from unrelated live processes.
		require.Empty(t, lookup.resolve(Pane{Command: "node", PID: 1_000_000_000 + i}))
	}
	require.Zero(t, lookup.remaining)
	require.Len(t, lookup.cache, maxProgramPanes)
}

func BenchmarkProgramLookupTenThousandUnknownNodePanes(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		lookup := newProgramLookup(context.Background())
		for i := range 10_000 {
			lookup.resolve(Pane{Command: "node", PID: 1_000_000_000 + i})
		}
	}
}
