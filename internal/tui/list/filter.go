package list

import "maps"

func (m Model) Expansion() map[ItemID]bool { return maps.Clone(m.expanded) }

func (m *Model) RestoreExpansion(expanded map[ItemID]bool) {
	m.expanded = cloneExpanded(expanded, IDs(m.snapshot.Items))
	if expanded == nil {
		markAllExpanded(m.snapshot.Items, m.expanded)
	}
	m.applyFilter()
	m.reflow()
}

func projectItems(items []Item, included map[ItemID]struct{}, details map[ItemID]Details) []Item {
	if included == nil && len(details) == 0 {
		return items
	}
	var result []Item
	for _, item := range items {
		children := projectItems(item.Children, included, details)
		if _, ok := included[item.ID]; included != nil && !ok && len(children) == 0 {
			continue
		}
		item.Children = children
		if detail, ok := details[item.ID]; ok {
			item.Secondary = detail.Secondary
			item.Trailing = detail.Trailing
			item.SearchFields = detail.SearchFields
		}
		result = append(result, item)
	}
	return result
}
