package list

import (
	"maps"
	"slices"
)

// Details replaces a row's secondary presentation without changing its identity.
type Details struct {
	Secondary    string
	Trailing     string
	SearchFields []SearchField
}

// SetDetails replaces presentation overrides and refilters while preserving selection
// and expansion. Callers must not mutate the supplied details afterward.
func (m *Model) SetDetails(details map[ItemID]Details) {
	if maps.EqualFunc(m.details, details, equalDetails) {
		return
	}
	selected, ok := m.Selected()
	m.details = details
	m.applyFilter()
	if ok {
		m.Select(selected.Item.ID)
	}
	m.reflow()
}

func equalDetails(a, b Details) bool {
	return a.Secondary == b.Secondary && a.Trailing == b.Trailing && slices.Equal(a.SearchFields, b.SearchFields)
}
