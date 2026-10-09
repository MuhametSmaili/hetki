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

func restrictItems(items []Item, included map[ItemID]struct{}) []Item {
	if included == nil {
		return items
	}
	var result []Item
	for _, item := range items {
		children := restrictItems(item.Children, included)
		if _, ok := included[item.ID]; !ok && len(children) == 0 {
			continue
		}
		item.Children = children
		result = append(result, item)
	}
	return result
}
