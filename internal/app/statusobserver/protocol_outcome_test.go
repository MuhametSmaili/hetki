package statusobserver

import (
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

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
		{"render zones", append(shellMarker("A"), shellMarker("C")...), status.Unknown, status.Completed},
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
