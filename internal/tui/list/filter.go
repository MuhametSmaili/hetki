package list

import (
	"context"
	"maps"
)

func (m Model) Expansion() map[ItemID]bool { return maps.Clone(m.expanded) }

func (m *Model) RestoreExpansion(expanded map[ItemID]bool) {
	ctx := m.operationContext()
	m.expanded = cloneExpanded(ctx, expanded, itemIDs(ctx, m.snapshot.Items))
	if expanded == nil {
		markAllExpanded(ctx, m.snapshot.Items, m.expanded)
	}
	m.applyFilter()
	m.reflow()
}

func projectItems(ctx context.Context, items []Item, included map[ItemID]struct{}, details map[ItemID]Details) []Item {
	if included == nil {
		return projectDetails(ctx, items, details)
	}
	var selected []projectedItem
	for i := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return nil
		}
		item := &items[i]
		children := projectItems(ctx, item.Children, included, details)
		if _, ok := included[item.ID]; !ok && len(children) == 0 {
			continue
		}
		selected = append(selected, projectedItem{item: item, children: children})
	}
	if len(selected) == 0 {
		return nil
	}
	result := make([]Item, len(selected))
	for i, entry := range selected {
		if i%64 == 0 && ctx.Err() != nil {
			return nil
		}
		result[i] = projectItem(entry.item, entry.children, details)
	}
	return result
}

// Scope discovery grows compact references, then allocates full items once the
// retained count is known. Sparse scopes allocate for retained items only.
type projectedItem struct {
	item     *Item
	children []Item
}

func projectDetails(ctx context.Context, items []Item, details map[ItemID]Details) []Item {
	if len(details) == 0 {
		return items
	}
	if len(items) == 0 {
		return nil
	}
	result := make([]Item, len(items))
	for i := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return nil
		}
		children := projectDetails(ctx, items[i].Children, details)
		result[i] = projectItem(&items[i], children, details)
	}
	return result
}

func projectItem(item *Item, children []Item, details map[ItemID]Details) Item {
	result := *item
	result.Children = children
	if detail, ok := details[item.ID]; ok {
		result.Secondary = detail.Secondary
		result.Trailing = detail.Trailing
		result.SearchFields = detail.SearchFields
	}
	return result
}
