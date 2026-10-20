package statusobserver

import (
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
	"github.com/MuhametSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func TestViewUsesCachedReportedPanesAndOwnsLocations(t *testing.T) {
	s, row := sourceFixture(t)
	s.locations = []observe.Location{{Session: "$1", Window: "@1", Pane: row.facts.ID}, {Session: "$2", Window: "@1", Pane: row.facts.ID}}
	empty, err := s.ReadView(t.Context())
	require.NoError(t, err)
	require.Empty(t, empty.Locations)
	scope := sourceScope(row.facts)
	s.Reset(scope)
	target, ok := s.core.Target(row.pane.Ref, service.Protocol)
	require.True(t, ok)
	require.NoError(t, s.core.Apply(target, 1, status.Value{State: status.Working}))

	view, err := s.ReadView(t.Context())
	require.NoError(t, err)
	view.Locations[0].Session = "changed"
	again, err := s.ReadView(t.Context())

	require.NoError(t, err)
	require.Len(t, again.Locations, 2)
	require.Equal(t, "$1", again.Locations[0].Session)
	require.Equal(t, status.Working, again.Values.Leased(again.Lease).At(row.pane.Ref, time.Now()))
	s.validatedUntil = time.Now()
	_, err = s.ReadView(t.Context())
	require.ErrorIs(t, err, daemon.ErrUnavailable)
}

func TestInventoryRenewalMovesOnlyTheLease(t *testing.T) {
	s, row := sourceFixture(t)
	s.locations = []observe.Location{{Session: "$1", Window: "@1", Pane: row.facts.ID}}
	scope := sourceScope(row.facts)
	s.Reset(scope)
	require.NoError(t, s.Output(scope, []byte("\x1b]7501;state=working\a")))
	before, err := s.ReadView(t.Context())
	require.NoError(t, err)

	s.revision++
	require.NoError(t, s.core.UpdateInventory(service.Inventory{
		Revision:   s.revision,
		ValidUntil: before.Lease.Add(time.Second),
		Panes:      []service.Pane{s.panes[row.facts.ID].pane},
	}))
	after, err := s.ReadView(t.Context())

	require.NoError(t, err)
	require.Equal(t, before.Values, after.Values, "a renewal must not change evidence")
	require.Equal(t, before.Locations, after.Locations)
	require.True(t, after.Lease.After(before.Lease))
}
