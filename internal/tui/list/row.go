package list

// rowRef borrows an item from the immutable snapshot or scope projection. Full
// rows and tree-prefix strings are materialized only when a caller needs them.
type rowRef struct {
	item      *Item
	score     int
	depth     int
	ancestors [MaxDepth]bool
	hasNext   bool
	expanded  bool
}

func newRowRef(item *Item, ancestors []bool, hasNext, expanded bool, score int) rowRef {
	row := rowRef{item: item, score: score, depth: len(ancestors), hasNext: hasNext, expanded: expanded}
	copy(row.ancestors[:], ancestors)
	return row
}

func (row rowRef) materialize() Row {
	return Row{
		Item:       *row.item,
		Depth:      row.depth,
		TreePrefix: treePrefix(row.ancestors[:row.depth], row.hasNext),
		Expanded:   row.expanded,
		Branch:     len(row.item.Children) > 0,
		Score:      row.score,
	}
}

func materializeRows(refs []rowRef) []Row {
	if refs == nil {
		return nil
	}
	rows := make([]Row, len(refs))
	for i, ref := range refs {
		rows[i] = ref.materialize()
	}
	return rows
}
