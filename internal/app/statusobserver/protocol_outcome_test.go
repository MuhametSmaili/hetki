package statusobserver

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestNewSuccessAfterAgedOutcomeAcrossChunkBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		reset []byte
	}{
		{"working", protocolReport("state=working")},
		{"clear", protocolReport("state=clear")},
		{"idle", protocolReport("state=idle")},
		{"blocked", protocolReport("state=blocked")},
		{"error", protocolReport("state=error")},
		{"terminal reset", []byte("\x1bc")},
	}
	for _, tc := range cases {
		input := append(tc.reset, protocolReport("state=done")...)
		for split := 0; split <= len(input); split++ {
			t.Run(fmt.Sprintf("%s/split=%d", tc.name, split), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					assertFreshSuccessAfterChunks(t, input[:split], input[split:])
				})
			})
		}
	}
}

func assertFreshSuccessAfterChunks(t *testing.T, chunks ...[]byte) {
	t.Helper()
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=done")))
	first, err := s.ReadStatus(t.Context())
	require.NoError(t, err)
	rowID := list.ItemID{ID: "row"}
	bindings := ui.StatusBindings{rowID: {row.pane.Ref}}
	time.Sleep(6 * time.Second)
	require.Equal(t, status.Idle, bindings.ValueAt(rowID, first, time.Now()).State)

	for _, chunk := range chunks {
		require.NoError(t, s.Output(scope, chunk))
	}
	current, err := s.ReadStatus(t.Context())

	require.NoError(t, err)
	require.Equal(t, time.Now(), current.Lookup(row.pane.Ref).OutcomeSince)
	require.True(t, current.Lookup(row.pane.Ref).OutcomeSince.After(first.Lookup(row.pane.Ref).OutcomeSince))
	require.Equal(t, status.Completed, bindings.ValueAt(rowID, current, time.Now().Add(5*time.Second-time.Nanosecond)).Outcome)
	require.Equal(t, status.Idle, bindings.ValueAt(rowID, current, time.Now().Add(5*time.Second)).State)
}

func TestProtocolOutcomesReachCacheAndEndOnNewEvidence(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	cases := []struct {
		name    string
		input   []byte
		state   status.State
		outcome status.Outcome
	}{
		{"working", protocolReport("state=working"), status.Working, status.NoOutcome},
		{"success", protocolReport("state=done"), status.Unknown, status.Completed},
		{"error", protocolReport("state=error"), status.Unknown, status.Failed},
		{"fresh activity", protocolReport("state=working"), status.Working, status.NoOutcome},
		{"another success", protocolReport("state=done"), status.Unknown, status.Completed},
		{"clear", protocolReport("state=clear"), status.Withdrawn, status.NoOutcome},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, s.Output(scope, tc.input))
			updateProtocolFacts(t, s, row.facts, Selection{}, true)

			snapshot, err := s.ReadStatus(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.state, snapshot.At(row.pane.Ref, time.Now()))
			require.Equal(t, tc.outcome, snapshot.Lookup(row.pane.Ref).OutcomeAt(time.Now()))
		})
	}
}

func TestOutcomeSurvivesVerifiedShellReturnButNotStreamLoss(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=done")))
	facts := row.facts
	facts.Foreground = facts.Root

	updateProtocolFacts(t, s, facts, Selection{}, true)
	scope.Foreground = facts.Foreground
	scope.Generation++
	s.Reset(scope)

	snapshot, err := s.ReadStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, status.Completed, snapshot.Lookup(row.pane.Ref).OutcomeAt(time.Now()))
	scope.Connection = "replacement"
	scope.Generation++
	s.Reset(scope)
	snapshot, err = s.ReadStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, status.NoOutcome, snapshot.Lookup(row.pane.Ref).Outcome)
}
