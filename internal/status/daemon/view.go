package daemon

import (
	"context"
	"fmt"

	"github.com/MSmaili/hetki/internal/status"
)

const maxLocations = 8192

// Location identifies one cached pane occurrence using backend object IDs.
// Pane resolves to exactly one incarnation in the accompanying snapshot.
type Location struct {
	Session string `json:"session"`
	Window  string `json:"window"`
	Pane    string `json:"pane"`
}

// View couples cached evidence and topology from the same inventory generation.
// Locations are caller-owned and include only panes present in Values.
type View struct {
	Values    status.Snapshot
	Locations []Location
}

func (v View) validate(server string) error {
	if len(v.Locations) > maxLocations {
		return fmt.Errorf("too many status locations")
	}
	panes := make(map[string]bool, v.Values.Len())
	for ref := range v.Values.All() {
		if ref.Server != server || panes[ref.ID] {
			return ErrIdentity
		}
		panes[ref.ID] = true
	}
	seen := make(map[Location]bool, len(v.Locations))
	for _, location := range v.Locations {
		if !validIdentity(location.Session) || !validIdentity(location.Window) || !panes[location.Pane] || seen[location] {
			return fmt.Errorf("invalid or duplicate status location")
		}
		seen[location] = true
	}
	return nil
}

// ReadView reads evidence and pane locations without triggering acquisition.
func (c *Client) ReadView(ctx context.Context) (View, error) {
	row, err := c.call(ctx, "view")
	if err != nil {
		return View{}, err
	}
	values, err := row.snapshot()
	if err != nil {
		return View{}, err
	}
	view := View{Values: values, Locations: row.Locations}
	if err := view.validate(c.server); err != nil {
		return View{}, err
	}
	return view, nil
}

func (d *daemon) readView(ctx context.Context, row *reply) error {
	source, ok := d.source.(interface {
		ReadView(context.Context) (View, error)
	})
	if !ok {
		return ErrUnavailable
	}
	view, err := source.ReadView(ctx)
	if err != nil {
		return err
	}
	if err := view.validate(d.binding.Server); err != nil {
		return err
	}
	row.Entries, err = snapshotEntries(d.binding.Server, view.Values)
	if err != nil {
		return err
	}
	row.Locations = view.Locations
	return nil
}
