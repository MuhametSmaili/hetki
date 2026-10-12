package statusobserver

import (
	"testing"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestChildLaunchPreservesReportsReceivedBeforeInventory(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
		want  status.State
	}{
		{"idle", protocolReport("state=idle"), status.Idle},
		{"working", protocolReport("state=working"), status.Working},
		{"blocked", protocolReport("state=blocked"), status.Blocked},
		{"no report", []byte("ordinary output"), status.Unreported},
		{"clear", protocolReport("state=clear"), status.Withdrawn},
		{"completion remains unknown activity", protocolReport("state=done"), status.Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, row := sourceFixture(t)
			root := row.facts
			root.Foreground = root.Root
			updateProtocolFacts(t, s, root, Selection{}, true)
			scope := sourceScope(root)
			s.Reset(scope)
			require.NoError(t, s.Output(scope, tc.input))
			old, ok := s.core.Target(row.pane.Ref, service.Protocol)
			require.True(t, ok)

			updateProtocolFacts(t, s, row.facts, Selection{}, true)
			scope.Foreground = row.facts.Foreground
			scope.Generation++
			s.Reset(scope)
			updateProtocolFacts(t, s, row.facts, Selection{}, true)

			requireSourceState(t, s, row.pane.Ref, tc.want)
			require.ErrorIs(t, s.core.Apply(old, s.protocolSequence+1, status.Value{State: status.Idle}), service.ErrStale)
			require.NoError(t, s.Output(scope, protocolReport("state=blocked")))
			updateProtocolFacts(t, s, row.facts, Selection{}, true)
			requireSourceState(t, s, row.pane.Ref, status.Blocked)
		})
	}
}

func TestChildLaunchPreservesFramingOnContinuousStream(t *testing.T) {
	s, row := sourceFixture(t)
	root := row.facts
	root.Foreground = root.Root
	updateProtocolFacts(t, s, root, Selection{}, true)
	scope := sourceScope(root)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("\x1b]7501;state=work")))

	updateProtocolFacts(t, s, row.facts, Selection{}, true)
	scope.Foreground = row.facts.Foreground
	scope.Generation++
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("ing\x1b\\")))

	requireSourceState(t, s, row.pane.Ref, status.Working)
}

func TestChildLaunchCannotPreserveReportsAcrossConnectionLoss(t *testing.T) {
	s, row := sourceFixture(t)
	root := row.facts
	root.Foreground = root.Root
	updateProtocolFacts(t, s, root, Selection{}, true)
	scope := sourceScope(root)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, protocolReport("state=working")))
	updateProtocolFacts(t, s, row.facts, Selection{}, true)
	scope.Foreground = row.facts.Foreground
	scope.Generation++
	scope.Connection = "replacement"

	s.Reset(scope)

	requireSourceState(t, s, row.pane.Ref, status.Unknown)
	require.NoError(t, s.Output(scope, protocolReport("state=idle")))
	requireSourceState(t, s, row.pane.Ref, status.Idle)
}
