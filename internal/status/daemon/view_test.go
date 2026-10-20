package daemon

import (
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func TestViewRoundTripPreservesLinkedOccurrences(t *testing.T) {
	client := fixtureClient(t, "view")
	ref := status.PaneRef{Server: client.server, ID: "%1", Generation: "birth"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		ref: {State: status.Working, ValidUntil: time.Now().UTC().Add(time.Minute)},
	})
	require.NoError(t, err)
	locations := []Location{{Session: "$1", Window: "@1", Pane: "%1"}, {Session: "$2", Window: "@1", Pane: "%1"}}
	source := newChangingSource(View{Values: values, Locations: locations}, nil)
	startFixture(t, client, liveBinding(client.server), source.factory())

	view, err := subscribe(t, client).Next()

	require.NoError(t, err)
	require.Equal(t, values.Lookup(ref), view.Values.Lookup(ref))
	require.Equal(t, locations, view.Locations)
}

func TestViewRejectsAmbiguousOrUnboundLocations(t *testing.T) {
	ref := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{ref: {State: status.Unknown}})
	require.NoError(t, err)
	location := Location{Session: "$1", Window: "@1", Pane: "%1"}
	for name, locations := range map[string][]Location{
		"missing pane":    {{Session: "$1", Window: "@1", Pane: "%2"}},
		"missing session": {{Window: "@1", Pane: "%1"}},
		"duplicate":       {location, location},
		"over limit":      make([]Location, maxLocations+1),
	} {
		t.Run(name, func(t *testing.T) {
			err := (View{Values: values, Locations: locations}).validate("server")

			require.Error(t, err)
		})
	}
	other := ref
	other.Generation = "replacement"
	ambiguous, err := status.NewSnapshot(map[status.PaneRef]status.Value{ref: {State: status.Unknown}, other: {State: status.Unknown}})
	require.NoError(t, err)

	require.ErrorIs(t, (View{Values: ambiguous}).validate("server"), ErrIdentity)
	require.ErrorIs(t, (View{Values: values}).validate("other-server"), ErrIdentity)
}
