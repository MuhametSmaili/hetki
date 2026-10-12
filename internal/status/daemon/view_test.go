package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

type viewSource struct {
	fixtureSource
	view View
}

func (s viewSource) ReadView(context.Context) (View, error) { return s.view, nil }

func TestViewRoundTripPreservesLinkedOccurrences(t *testing.T) {
	client := fixtureClient(t, "view")
	ref := status.PaneRef{Server: client.server, ID: "%1", Generation: "birth"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		ref: {State: status.Working, ValidUntil: time.Now().UTC().Add(time.Minute)},
	})
	require.NoError(t, err)
	locations := []Location{{Session: "$1", Window: "@1", Pane: "%1"}, {Session: "$2", Window: "@1", Pane: "%1"}}
	startFixture(t, client, liveBinding(client.server), func(context.Context, func(error)) (Source, error) {
		return viewSource{fixtureSource: fixtureSource{close: func() error { return nil }}, view: View{Values: values, Locations: locations}}, nil
	})

	view, err := client.ReadView(t.Context())

	require.NoError(t, err)
	require.Equal(t, values.Lookup(ref), view.Values.Lookup(ref))
	require.Equal(t, locations, view.Locations)
	watch, err := client.SupportsWatch(t.Context())
	require.NoError(t, err)
	require.False(t, watch, "read-only sources do not advertise change delivery")
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
