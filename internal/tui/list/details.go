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
	if !maps.EqualFunc(m.details, details, equalSearchFields) {
		m.invalidateSearch()
	}
	m.details = details
	m.projection = nil
	m.searchKey.presentation++
	m.applyFilter()
	if ok {
		m.PreferSelection(selected.Item.ID)
	}
	m.reflow()
}

func equalDetails(a, b Details) bool {
	return a.Secondary == b.Secondary && a.Trailing == b.Trailing && slices.Equal(a.SearchFields, b.SearchFields)
}

func equalSearchFields(a, b Details) bool {
	return slices.Equal(a.SearchFields, b.SearchFields)
}
