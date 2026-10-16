package statusobserver

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func TestApplicationPromptMarkersPreserveExplicitActivity(t *testing.T) {
	for _, state := range []status.State{status.Working, status.Idle, status.Blocked} {
		t.Run(string(state), func(t *testing.T) {
			s, row := sourceFixture(t)
			scope := sourceScope(row.facts)
			s.Reset(scope)
			require.NoError(t, s.Output(scope, protocolReport("state="+string(state))))
			sequence := s.protocolSequence

			require.NoError(t, s.Output(scope, shellMarker("A")))
			updateProtocolFacts(t, s, row.facts, Selection{}, true)
			require.NoError(t, s.Output(scope, append(shellMarker("B"), shellMarker("C")...)))
			updateProtocolFacts(t, s, row.facts, Selection{}, true)

			requireSourceState(t, s, row.pane.Ref, state)
			require.Equal(t, sequence, s.protocolSequence, "render markers must not republish activity")
		})
	}
}

func TestRootPromptRequiresInventoryStartedAfterMarker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, row := sourceFixture(t)
		facts := row.facts
		facts.Foreground = facts.Root
		updateProtocolFacts(t, s, facts, Selection{}, true)
		scope := sourceScope(facts)
		s.Reset(scope)
		require.NoError(t, s.Output(scope, protocolReport("state=working")))
		startedAt := time.Now()
		time.Sleep(time.Millisecond)

		require.NoError(t, s.Output(scope, shellMarker("A")))
		updateProtocolFactsAt(t, s, facts, Selection{}, true, startedAt)

		requireSourceState(t, s, row.pane.Ref, status.Working)
		updateProtocolFacts(t, s, facts, Selection{}, true)
		requireSourceState(t, s, row.pane.Ref, status.Withdrawn)
	})
}

func TestNewEvidenceOrExecutionCancelsPendingPrompt(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
		want  status.State
	}{
		{"fresh report", protocolReport("state=blocked"), status.Blocked},
		{"render zone end", append(shellMarker("B"), shellMarker("C")...), status.Working},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, row := sourceFixture(t)
			facts := row.facts
			facts.Foreground = facts.Root
			updateProtocolFacts(t, s, facts, Selection{}, true)
			scope := sourceScope(facts)
			s.Reset(scope)
			require.NoError(t, s.Output(scope, protocolReport("state=working")))
			require.NoError(t, s.Output(scope, shellMarker("A")))

			require.NoError(t, s.Output(scope, tc.input))
			updateProtocolFacts(t, s, facts, Selection{}, true)

			requireSourceState(t, s, row.pane.Ref, tc.want)
		})
	}
}

func TestNewPromptAfterFreshReportNeedsNewValidation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, row := sourceFixture(t)
		facts := row.facts
		facts.Foreground = facts.Root
		updateProtocolFacts(t, s, facts, Selection{}, true)
		scope := sourceScope(facts)
		s.Reset(scope)
		require.NoError(t, s.Output(scope, protocolReport("state=working")))
		require.NoError(t, s.Output(scope, shellMarker("A")))
		startedAt := time.Now()
		time.Sleep(time.Millisecond)
		input := append(protocolReport("state=blocked"), shellMarker("A")...)

		require.NoError(t, s.Output(scope, input))
		updateProtocolFactsAt(t, s, facts, Selection{}, true, startedAt)

		requireSourceState(t, s, row.pane.Ref, status.Blocked)
		updateProtocolFacts(t, s, facts, Selection{}, true)
		requireSourceState(t, s, row.pane.Ref, status.Withdrawn)
	})
}
