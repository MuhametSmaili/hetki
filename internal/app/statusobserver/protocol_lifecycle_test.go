package statusobserver

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestProtocolWithdrawalSurvivesForegroundReconciliation(t *testing.T) {
	cases := []struct {
		name   string
		before []byte
		after  []byte
	}{
		{"prompt before inventory", shellMarker("A"), nil},
		{"prompt after inventory", nil, shellMarker("A")},
		{"native handoff without prompt", nil, nil},
		{"explicit clear before inventory", protocolReport("state=clear"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, row := sourceFixture(t)
			scope := sourceScope(row.facts)
			s.Reset(scope)
			require.NoError(t, s.Output(scope, protocolReport("state=working")))
			old, ok := s.core.Target(row.pane.Ref, service.Protocol)
			require.True(t, ok)
			require.NoError(t, s.Output(scope, tc.before))
			facts := row.facts
			facts.Foreground = facts.Root

			updateProtocolFacts(t, s, facts, Selection{}, true)
			scope.Foreground = facts.Foreground
			scope.Generation++
			s.Reset(scope)
			require.NoError(t, s.Output(scope, tc.after))
			sequence := s.protocolSequence
			for range 3 {
				updateProtocolFacts(t, s, facts, Selection{}, true)
			}

			requireSourceState(t, s, row.pane.Ref, status.Withdrawn)
			require.Equal(t, sequence, s.protocolSequence, "inventory must not replay withdrawal")
			require.ErrorIs(t, s.core.Apply(old, sequence+1, status.Value{State: status.Working}), service.ErrStale)
			require.NoError(t, s.Output(scope, protocolReport("state=blocked")))
			updateProtocolFacts(t, s, facts, Selection{}, true)
			s.Reset(scope)
			requireSourceState(t, s, row.pane.Ref, status.Blocked)
		})
	}
}

func TestPendingWithdrawalCannotCrossLostContinuity(t *testing.T) {
	cases := []struct {
		name      string
		interrupt func(*testing.T, *Source, observe.Pane, *Scope)
	}{
		{
			name: "new connection",
			interrupt: func(_ *testing.T, _ *Source, _ observe.Pane, scope *Scope) {
				scope.Connection = "replacement"
			},
		},
		{
			name: "expired validation",
			interrupt: func(t *testing.T, s *Source, facts observe.Pane, _ *Scope) {
				time.Sleep(time.Minute + time.Second)
				updateProtocolFacts(t, s, facts, Selection{}, true)
			},
		},
		{
			name: "failed inventory",
			interrupt: func(t *testing.T, s *Source, facts observe.Pane, _ *Scope) {
				require.NoError(t, s.publishLocked(observe.Inventory{}))
				updateProtocolFacts(t, s, facts, Selection{}, true)
			},
		},
		{
			name: "new profile",
			interrupt: func(t *testing.T, s *Source, facts observe.Pane, _ *Scope) {
				selected := Selection{Profile: service.Profile{Source: service.Protocol, Generation: "replacement"}}
				updateProtocolFacts(t, s, facts, selected, true)
			},
		},
		{
			name: "missing native facts",
			interrupt: func(t *testing.T, s *Source, facts observe.Pane, _ *Scope) {
				missing := facts
				missing.Foreground = observe.Process{}
				updateProtocolFacts(t, s, missing, Selection{}, false)
				updateProtocolFacts(t, s, facts, Selection{}, true)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, row := sourceFixture(t)
				scope := sourceScope(row.facts)
				s.Reset(scope)
				require.NoError(t, s.Output(scope, protocolReport("state=working")))
				require.NoError(t, s.Output(scope, shellMarker("A")))
				facts := row.facts
				facts.Foreground = facts.Root
				updateProtocolFacts(t, s, facts, Selection{}, true)
				scope.Foreground = facts.Foreground
				scope.Generation++

				tc.interrupt(t, s, facts, &scope)
				s.Reset(scope)

				requireSourceState(t, s, row.pane.Ref, status.Unknown)
				require.NoError(t, s.Output(scope, protocolReport("state=idle")))
				requireSourceState(t, s, row.pane.Ref, status.Idle)
			})
		})
	}
}

func TestProfileReleaseRequiresValidatedSelection(t *testing.T) {
	s, row := sourceFixture(t)
	selection := Selection{Profile: service.Profile{Source: service.Screen, Generation: "screen"}}
	updateProtocolFacts(t, s, row.facts, selection, true)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	target, ok := s.core.Target(row.pane.Ref, service.Screen)
	require.True(t, ok)
	require.NoError(t, s.core.Apply(target, 1, status.Value{State: status.Working, ValidUntil: time.Now().Add(time.Minute)}))
	facts := row.facts
	facts.Command = "unprofiled-command"
	facts.Foreground = facts.Root

	unavailable := updateProtocolFacts(t, s, facts, Selection{}, false)

	require.Equal(t, selection.Profile, unavailable.pane.Profile)
	requireSourceState(t, s, row.pane.Ref, status.Unknown)
	updateProtocolFacts(t, s, facts, Selection{}, true)
	scope.Foreground = facts.Foreground
	scope.Generation++
	s.Reset(scope)
	require.NoError(t, s.Output(scope, shellMarker("A")))
	requireSourceState(t, s, row.pane.Ref, status.Unreported)
	require.ErrorIs(t, s.core.Apply(target, 2, status.Value{State: status.Unknown}), service.ErrStale)
}

func TestMissingMetadataInNewPaneDoesNotInheritOldProfile(t *testing.T) {
	s, row := sourceFixture(t)
	selection := Selection{Profile: service.Profile{Source: service.Screen, Generation: "screen"}}
	updateProtocolFacts(t, s, row.facts, selection, true)
	facts := row.facts
	facts.RootPID++
	facts.Root = observe.Process{}
	facts.Foreground = observe.Process{}

	replacement := updateProtocolFacts(t, s, facts, Selection{}, false)

	require.NotEqual(t, row.pane.Ref, replacement.pane.Ref)
	require.Empty(t, replacement.pane.Profile)
	requireSourceState(t, s, replacement.pane.Ref, status.Unreported)
}

func TestPendingWithdrawalEndsWithPaneLifetime(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=working")))
	facts := row.facts
	facts.Foreground = facts.Root
	updateProtocolFacts(t, s, facts, Selection{}, true)
	facts.Root.Birth = "new-root"
	facts.Foreground = facts.Root

	replacement := updateProtocolFacts(t, s, facts, Selection{}, true)
	scope.Root, scope.Foreground = facts.Root, facts.Foreground
	scope.Generation++
	s.Reset(scope)
	require.NoError(t, s.Output(scope, shellMarker("A")))

	require.NotEqual(t, row.pane.Ref, replacement.pane.Ref)
	requireSourceState(t, s, replacement.pane.Ref, status.Unreported)
}

func TestReturningForegroundProcessRequiresFreshReport(t *testing.T) {
	s, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=working")))
	old, ok := s.core.Target(row.pane.Ref, service.Protocol)
	require.True(t, ok)
	shell := row.facts
	shell.Foreground = shell.Root
	updateProtocolFacts(t, s, shell, Selection{}, true)
	scope.Foreground = shell.Foreground
	scope.Generation++
	s.Reset(scope)

	// Resuming a job restores its process identity, not its previous evidence generation.
	updateProtocolFacts(t, s, row.facts, Selection{}, true)
	scope.Foreground = row.facts.Foreground
	scope.Generation++
	s.Reset(scope)

	requireSourceState(t, s, row.pane.Ref, status.Withdrawn)
	require.ErrorIs(t, s.core.Apply(old, s.protocolSequence+1, status.Value{State: status.Working}), service.ErrStale)
	require.NoError(t, s.Output(scope, protocolReport("state=idle")))
	requireSourceState(t, s, row.pane.Ref, status.Idle)
}
