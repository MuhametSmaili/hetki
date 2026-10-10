package statusobserver

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/protocols"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func protocolReport(payload string) []byte {
	return []byte("\x1b]7501;" + payload + "\x1b\\")
}

func shellMarker(payload string) []byte { return []byte("\x1b]133;" + payload + "\a") }

func TestShellInvocationSurvivesVerifiedForegroundHandoff(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, shellMarker("C")))
	before := s.panes[row.facts.ID].protocol.pane.Snapshot().Shell
	facts := row.facts
	facts.Foreground.Birth = "child"

	updateProtocolFacts(t, s, facts, Selection{}, true)
	scope.Foreground = facts.Foreground
	scope.Generation++
	s.Reset(scope)
	require.NoError(t, s.Output(scope, shellMarker("D;130")))
	after := s.panes[row.facts.ID].protocol.pane.Snapshot().Shell

	require.Equal(t, before.Invocation, after.Invocation)
	require.Equal(t, protocols.Finished, after.Phase)
	require.True(t, after.ExitKnown)
	require.Equal(t, uint8(130), after.ExitCode)
	requireSourceState(t, s, row.pane.Ref, status.Unreported)
}

func TestExpiredInventoryCannotPreserveCommandAcrossHandoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, row := sourceFixture(t)
		scope := sourceScope(row.facts)
		s.Reset(scope)
		require.NoError(t, s.Output(scope, shellMarker("C")))
		facts := row.facts
		facts.Foreground.Birth = "next"

		time.Sleep(time.Minute + time.Second)
		updateProtocolFacts(t, s, facts, Selection{}, true)
		scope.Foreground = facts.Foreground
		scope.Generation++
		s.Reset(scope)
		require.NoError(t, s.Output(scope, shellMarker("D;0")))

		require.Equal(t, protocols.Shell{}, s.panes[row.facts.ID].protocol.pane.Snapshot().Shell)
	})
}

func TestShellResetDiscardsPartialMarkersAndCommandMatching(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, shellMarker("C")))
	require.NoError(t, s.Output(scope, []byte("\x1b]133;D;")))

	scope.Connection = "replacement"
	scope.Generation++
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("0\a")))
	require.NoError(t, s.Output(scope, shellMarker("D;0")))

	require.Equal(t, protocols.Shell{}, s.panes[row.facts.ID].protocol.pane.Snapshot().Shell)
}

func TestScreenSelectionTracksShellWithoutAcceptingOSCActivity(t *testing.T) {
	s, row := sourceFixture(t)
	s.screens = newScreens(s.core, successfulScreens)
	selection := Selection{Profile: service.Profile{Source: service.Screen, Generation: "rules"}}
	updateProtocolFacts(t, s, row.facts, selection, true)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	input := append(shellMarker("C"), protocolReport("state=working")...)

	require.NoError(t, s.Output(scope, input))

	observation := s.panes[row.facts.ID].protocol.pane.Snapshot()
	require.Equal(t, protocols.Running, observation.Shell.Phase)
	require.Equal(t, status.Unreported, observation.Activity)
	requireSourceState(t, s, row.pane.Ref, status.Unknown)
}

func requireSourceState(t *testing.T, s *Source, ref status.PaneRef, state status.State) {
	t.Helper()
	snapshot, err := s.ReadStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, state, snapshot.At(ref, time.Now()))
}

func TestProtocolDispatchAndRootProjection(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	cases := []struct {
		name  string
		input []byte
		want  status.State
	}{
		{"query and unrelated OSC", append(protocolReport("?"), []byte("\x1b]2;working\a")...), status.Unreported},
		{"root working", protocolReport("state=working"), status.Working},
		{"blocked child does not replace root", protocolReport("state=blocked:id=child"), status.Working},
		{"invalid report is atomic", protocolReport("state=idle:msg=/w=="), status.Working},
		{"no heartbeat required", []byte("ordinary output"), status.Working},
		{"idle", protocolReport("state=idle"), status.Idle},
		{"blocked", protocolReport("state=blocked"), status.Blocked},
		{"done is not idle", protocolReport("state=done"), status.Unknown},
		{"error is not idle", protocolReport("state=error"), status.Unknown},
		{"clear is withdrawal", protocolReport("state=clear"), status.Withdrawn},
		{"unknown cannot resurrect evidence", protocolReport("state=future"), status.Withdrawn},
		{"new root", protocolReport("state=working"), status.Working},
		{"intermediate and soft reset preserve records", []byte("\x1b(c\x1b[!p\x1b[?1049h"), status.Working},
		{"RIS withdraws", []byte("\x1bc"), status.Withdrawn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, s.Output(scope, tc.input))

			requireSourceState(t, s, row.pane.Ref, tc.want)
		})
	}
}

func TestProtocolInputCannotCrossStreamReset(t *testing.T) {
	s, row := sourceFixture(t)
	before := sourceScope(row.facts)
	s.Reset(before)
	require.NoError(t, s.Output(before, protocolReport("state=working")))
	require.NoError(t, s.Output(before, []byte("\x1b]7501;state=idle")))
	after := before
	after.Generation++

	s.Reset(after)
	require.NoError(t, s.Output(before, protocolReport("state=blocked")))
	require.NoError(t, s.Output(after, []byte("\a")))

	requireSourceState(t, s, row.pane.Ref, status.Unknown)
	require.NoError(t, s.Output(after, protocolReport("state=idle")))
	requireSourceState(t, s, row.pane.Ref, status.Idle)
}

func TestProtocolPanesCannotSharePartialMessages(t *testing.T) {
	s, first := sourceFixture(t)
	secondFacts := sourceFacts()
	secondFacts.ID = "%2"
	second := s.reconcilePane(sourcePane{}, secondFacts, Selection{}, true)
	s.panes[secondFacts.ID] = second
	s.revision++
	require.NoError(t, s.core.UpdateInventory(service.Inventory{
		Revision:   s.revision,
		ValidUntil: time.Now().Add(time.Minute),
		Panes:      []service.Pane{first.pane, second.pane},
	}))
	firstScope, secondScope := sourceScope(first.facts), sourceScope(second.facts)
	s.Reset(firstScope)
	s.Reset(secondScope)

	require.NoError(t, s.Output(firstScope, []byte("\x1b]7501;state=work")))
	require.NoError(t, s.Output(secondScope, []byte("ing\a")))
	require.NoError(t, s.Output(secondScope, protocolReport("state=idle")))
	require.NoError(t, s.Output(firstScope, []byte("ing\a")))

	requireSourceState(t, s, first.pane.Ref, status.Working)
	requireSourceState(t, s, second.pane.Ref, status.Idle)
}

func TestProtocolResizePreservesPartialInputAndRecords(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("\x1b]7501;state=work")))
	facts := row.facts
	facts.Columns++

	row = updateProtocolFacts(t, s, facts, Selection{}, true)
	require.NoError(t, s.Output(scope, []byte("ing\a")))

	requireSourceState(t, s, row.pane.Ref, status.Working)
}

func updateProtocolFacts(t *testing.T, s *Source, facts observe.Pane, selected Selection, valid bool) sourcePane {
	t.Helper()
	s.expireProtocols(time.Now())
	row := s.reconcilePane(s.panes[facts.ID], facts, selected, valid)
	s.revision++
	err := s.core.UpdateInventory(service.Inventory{
		Revision:   s.revision,
		ValidUntil: time.Now().Add(time.Minute),
		Panes:      []service.Pane{row.pane},
	})
	require.NoError(t, err)
	s.panes[facts.ID] = row
	s.validatedUntil = time.Now().Add(time.Minute)
	if row.pane.Scope.Connection != "" {
		s.bindProtocol(row)
	}
	return row
}

func TestProtocolFreshInventoryCannotReviveExpiredPartialInput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, row := sourceFixture(t)
		scope := sourceScope(row.facts)
		s.Reset(scope)
		require.NoError(t, s.Output(scope, protocolReport("state=working")))
		require.NoError(t, s.Output(scope, []byte("\x1b]7501;state=idle")))

		time.Sleep(time.Minute + time.Second)
		updateProtocolFacts(t, s, row.facts, Selection{}, true)
		require.NoError(t, s.Output(scope, []byte("\a")))

		requireSourceState(t, s, row.pane.Ref, status.Unknown)
	})
}

func TestProtocolInventoryFailureDropsPartialInput(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=working")))
	require.NoError(t, s.Output(scope, []byte("\x1b]7501;state=idle")))

	require.NoError(t, s.publishLocked(observe.Inventory{}))
	updateProtocolFacts(t, s, row.facts, Selection{}, true)
	require.NoError(t, s.Output(scope, []byte("\a")))

	requireSourceState(t, s, row.pane.Ref, status.Unknown)
}

func TestProtocolOutcomeSurvivesConfirmedExitButNotRespawn(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=done")))
	require.NoError(t, s.Output(scope, protocolReport("state=working:id=child")))
	facts := row.facts
	facts.Dead = true
	facts.Root = observe.Process{}
	facts.Foreground = observe.Process{}

	dead := updateProtocolFacts(t, s, facts, Selection{}, false)
	s.Reset(Scope{Pane: facts.ID, Generation: 2})
	observation := dead.protocol.pane.Snapshot()

	require.Equal(t, protocols.Completed, observation.Outcome)
	requireSourceState(t, s, dead.pane.Ref, status.Unknown)
	respawn := updateProtocolFacts(t, s, row.facts, Selection{}, true)
	require.NotEqual(t, dead.pane.Ref, respawn.pane.Ref)
	require.Nil(t, respawn.protocol)
}

func TestProtocolForegroundHandoffRetainsOnlyOutcome(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=done")))
	require.NoError(t, s.Output(scope, protocolReport("state=blocked:id=child")))
	facts := row.facts
	facts.Foreground.Birth = "next-owner"

	row = updateProtocolFacts(t, s, facts, Selection{}, true)
	scope.Foreground = facts.Foreground
	scope.Generation++
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("text")))
	observation := row.protocol.pane.Snapshot()

	require.Equal(t, protocols.Completed, observation.Outcome)
	requireSourceState(t, s, row.pane.Ref, status.Unknown)
}

func TestMissingProcessMetadataDoesNotProveExitOrReviveOutcome(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=done")))
	facts := row.facts
	facts.Foreground = observe.Process{}

	missing := updateProtocolFacts(t, s, facts, Selection{}, false)
	s.Reset(Scope{Pane: facts.ID, Root: facts.Root, Generation: 2})
	outcome := missing.protocol.pane.Snapshot().Outcome
	updateProtocolFacts(t, s, row.facts, Selection{}, true)
	scope.Generation = 3
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("text")))

	require.Equal(t, protocols.NoOutcome, outcome)
	requireSourceState(t, s, row.pane.Ref, status.Unknown)
}

func TestScreenSelectionPreventsProtocolEvidenceAndRetirementClearsRecords(t *testing.T) {
	s, row := sourceFixture(t)
	s.screens = newScreens(s.core, successfulScreens)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=working")))
	selection := Selection{
		Profile: service.Profile{Source: service.Screen, Generation: "rules"},
		Evaluate: func(string) (status.State, error) {
			return status.Unknown, nil
		},
	}

	selected := updateProtocolFacts(t, s, row.facts, selection, true)
	require.NoError(t, s.Output(scope, protocolReport("state=idle")))

	require.Nil(t, selected.protocol)
	requireSourceState(t, s, row.pane.Ref, status.Unknown)
	updateProtocolFacts(t, s, row.facts, Selection{}, true)
	require.NoError(t, s.Output(scope, []byte("text")))
	requireSourceState(t, s, row.pane.Ref, status.Unknown)
}
