package daemon

import (
	"fmt"
	"slices"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
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
// Values from a Source do not include the Lease. Values from a Subscription do.
type View struct {
	Values    status.Snapshot
	Locations []Location
	Lease     time.Time
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

func (c *Client) decodeView(row reply) (View, error) {
	values, err := row.snapshot()
	if err != nil {
		return View{}, err
	}
	view := View{Values: values, Locations: row.Locations, Lease: row.Lease}
	if err := view.validate(c.server); err != nil {
		return View{}, err
	}
	return view, nil
}

// encodeView writes a full frame. The caller validates the view first.
func (d *daemon) encodeView(row *reply, view View) error {
	entries, err := snapshotEntries(d.binding.Server, view.Values)
	if err != nil {
		return err
	}
	row.Entries, row.Locations, row.Lease = entries, view.Locations, view.Lease
	return nil
}

// encodeRenewal writes only the lease and the deadlines that moved. Use it only
// when sameEvidence(sent, view) is true.
func encodeRenewal(row *reply, sent, view View) {
	row.Renewal, row.Lease = true, view.Lease
	for pane, value := range view.Values.All() {
		if !value.ValidUntil.Equal(sent.Values.Lookup(pane).ValidUntil) {
			row.Entries = append(row.Entries, entry{ID: pane.ID, Generation: pane.Generation, ValidUntil: value.ValidUntil})
		}
	}
}

// sameEvidence reports whether two views differ at most in deadlines and lease.
func sameEvidence(a, b View) bool {
	if a.Values.Len() != b.Values.Len() || !slices.Equal(a.Locations, b.Locations) {
		return false
	}
	for pane, value := range b.Values.All() {
		previous := a.Values.Lookup(pane)
		if previous.State != value.State || previous.Outcome != value.Outcome ||
			previous.OutcomeEnded != value.OutcomeEnded || !previous.OutcomeSince.Equal(value.OutcomeSince) {
			return false
		}
	}
	return true
}

// renew applies a renewal frame to the last full view.
func (c *Client) renew(last View, row reply) (View, error) {
	values := make(map[status.PaneRef]status.Value, last.Values.Len())
	for pane, value := range last.Values.All() {
		values[pane] = value
	}

	for _, e := range row.Entries {
		pane := status.PaneRef{Server: c.server, ID: e.ID, Generation: e.Generation}
		value, found := values[pane]
		if !found {
			return View{}, fmt.Errorf("status renewal names an unknown pane")
		}
		value.ValidUntil = e.ValidUntil
		values[pane] = value
	}

	snapshot, err := status.NewSnapshot(values)
	if err != nil {
		return View{}, err
	}
	return View{Values: snapshot, Locations: last.Locations, Lease: row.Lease}, nil
}
