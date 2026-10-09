package list

import (
	"maps"
	"slices"
	"strings"

	"github.com/sahilm/fuzzy"
)

const maxQuality = 999

type Row struct {
	Item       Item
	Depth      int
	TreePrefix string
	Expanded   bool
	Branch     bool
	Score      int
}

type Model struct {
	snapshot  Snapshot
	rows      []Row
	cursor    int
	offset    int
	height    int
	shownRoot int
	query     string
	included  map[ItemID]struct{}
	expanded  map[ItemID]bool
	active    map[ItemID]bool
}

func New(snapshot Snapshot) (Model, error) {
	if err := Validate(snapshot); err != nil {
		return Model{}, err
	}
	m := Model{snapshot: snapshot, expanded: make(map[ItemID]bool), active: activePath(snapshot.Items, snapshot.ActiveItemID)}
	markAllExpanded(snapshot.Items, m.expanded)
	markActivePathExpanded(snapshot.Items, snapshot.ActiveItemID, m.expanded)
	m.applyFilter()
	return m, nil
}

func (m Model) Snapshot() Snapshot      { return m.snapshot }
func (m Model) Rows() []Row             { return m.rows }
func (m Model) Cursor() int             { return m.cursor }
func (m Model) Offset() int             { return m.offset }
func (m Model) Height() int             { return m.height }
func (m Model) Query() string           { return m.query }
func (m Model) IsActive(id ItemID) bool { return m.active[id] }

func (m Model) ShownRoots() int { return m.shownRoot }

func (m Model) Selected() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

func (m Model) VisibleRows() []Row {
	start := m.offset
	end := min(len(m.rows), start+m.height)
	if start < 0 || start >= end {
		return nil
	}
	return m.rows[start:end]
}

func (m *Model) Replace(snapshot Snapshot, preferred ItemID) error {
	if err := Validate(snapshot); err != nil {
		return err
	}
	selectedID := preferred
	if selectedID == "" {
		if selected, ok := m.Selected(); ok {
			selectedID = selected.Item.ID
		}
	}
	candidate := *m
	candidate.snapshot = snapshot
	candidate.expanded = cloneExpanded(m.expanded, IDs(snapshot.Items))
	candidate.active = activePath(snapshot.Items, snapshot.ActiveItemID)
	markActivePathExpanded(snapshot.Items, snapshot.ActiveItemID, candidate.expanded)
	candidate.applyFilter()
	if idx := findRow(candidate.rows, selectedID); idx >= 0 {
		candidate.cursor = idx
	}
	candidate.reflow()
	*m = candidate
	return nil
}

func (m *Model) SetQuery(query string) {
	m.SetFilter(query, nil)
}

// SetFilter retains ancestors of included items; nil allows all items.
func (m *Model) SetFilter(query string, included map[ItemID]struct{}) bool {
	if m.query == query && (m.included == nil) == (included == nil) && maps.Equal(m.included, included) {
		return false
	}
	selected, hasSelection := m.Selected()
	preserve := m.query == query
	m.query = query
	m.included = maps.Clone(included)
	m.applyFilter()
	if preserve && hasSelection {
		m.Select(selected.Item.ID)
	}
	m.reflow()
	return true
}

func (m *Model) Resize(height int) {
	m.height = max(0, height)
	m.reflow()
}

func (m *Model) Move(delta int) {
	m.cursor = clampCursor(m.cursor+delta, len(m.rows))
	m.reflow()
}

func (m *Model) Top() {
	m.cursor = clampCursor(0, len(m.rows))
	m.reflow()
}

func (m *Model) Bottom() {
	m.cursor = clampCursor(len(m.rows)-1, len(m.rows))
	m.reflow()
}

func (m *Model) Page(delta int) {
	m.Move(delta * m.height)
}

func (m *Model) Select(id ItemID) bool {
	idx := findRow(m.rows, id)
	if idx < 0 {
		return false
	}
	m.cursor = idx
	m.reflow()
	return true
}

func (m *Model) SelectSurvivor(previous []ItemID, anchor ItemID) bool {
	position := slices.Index(previous, anchor)
	if position < 0 {
		return false
	}
	for _, id := range previous[position+1:] {
		if m.Select(id) {
			return true
		}
	}
	for i := position - 1; i >= 0; i-- {
		if m.Select(previous[i]) {
			return true
		}
	}
	return false
}

func (m *Model) ToggleSelected(expand bool) bool {
	selected, ok := m.Selected()
	if !ok || !selected.Branch || strings.TrimSpace(m.query) != "" {
		return false
	}
	m.expanded[selected.Item.ID] = expand
	m.applyFilter()
	m.reflow()
	return true
}

func (m *Model) CollapseAll() bool {
	if strings.TrimSpace(m.query) != "" {
		return false
	}
	m.expanded = make(map[ItemID]bool)
	m.applyFilter()
	m.reflow()
	return true
}

func (m *Model) ExpandAll() bool {
	if strings.TrimSpace(m.query) != "" {
		return false
	}
	markAllExpanded(m.snapshot.Items, m.expanded)
	m.applyFilter()
	m.reflow()
	return true
}

func (m *Model) JumpMatch(forward bool) bool {
	indices := m.matchIndices()
	if len(indices) == 0 {
		return false
	}
	next := indices[0]
	if forward {
		for _, idx := range indices {
			if idx > m.cursor {
				next = idx
				break
			}
		}
	} else {
		next = indices[len(indices)-1]
		for i := len(indices) - 1; i >= 0; i-- {
			if indices[i] < m.cursor {
				next = indices[i]
				break
			}
		}
	}
	m.cursor = next
	m.reflow()
	return true
}

func (m Model) MatchPosition() (int, int) {
	indices := m.matchIndices()
	if len(indices) == 0 {
		return 0, 0
	}
	for i, idx := range indices {
		if idx == m.cursor {
			return i + 1, len(indices)
		}
	}
	return 1, len(indices)
}

func (m *Model) applyFilter() {
	var selectedID ItemID
	if selected, ok := m.Selected(); ok {
		selectedID = selected.Item.ID
	}
	query := strings.ToLower(strings.TrimSpace(m.query))
	items := restrictItems(m.snapshot.Items, m.included)
	if query == "" {
		m.rows = flatten(items, m.expanded, false)
		if idx := findRow(m.rows, selectedID); idx >= 0 {
			m.cursor = idx
		} else {
			m.cursor = clampCursor(m.cursor, len(m.rows))
		}
	} else {
		m.rows = rankedMatches(items, query)
		m.cursor = bestMatchCursor(m.rows)
	}
	m.shownRoot = 0
	for _, row := range m.rows {
		if row.Depth == 0 {
			m.shownRoot++
		}
	}
}

func (m *Model) reflow() {
	m.cursor = clampCursor(m.cursor, len(m.rows))
	m.offset = clampOffset(m.offset, len(m.rows), m.height)
	m.offset = ensureCursorVisible(m.offset, m.cursor, len(m.rows), m.height)
}

func (m Model) matchIndices() []int {
	indices := make([]int, 0, len(m.rows))
	for i, row := range m.rows {
		if row.Score > 0 {
			indices = append(indices, i)
		}
	}
	return indices
}

func flatten(items []Item, expanded map[ItemID]bool, includeAll bool) []Row {
	rows := make([]Row, 0)
	flattenAtDepth(items, nil, expanded, includeAll, &rows)
	return rows
}

func flattenAtDepth(items []Item, ancestors []bool, expanded map[ItemID]bool, includeAll bool, rows *[]Row) {
	for i, item := range items {
		hasNext := i < len(items)-1
		isExpanded := includeAll || expanded[item.ID]
		*rows = append(*rows, Row{
			Item: item, Depth: len(ancestors), TreePrefix: treePrefix(ancestors),
			Expanded: isExpanded, Branch: len(item.Children) > 0,
		})
		if len(item.Children) > 0 && isExpanded {
			flattenAtDepth(item.Children, append(append([]bool(nil), ancestors...), hasNext), expanded, includeAll, rows)
		}
	}
}

type matchedItem struct {
	item     Item
	children []matchedItem
	score    int
	rank     int
}

func rankedMatches(items []Item, query string) []Row {
	matched := matchItems(items, query)
	rows := make([]Row, 0)
	flattenMatches(matched, nil, &rows)
	return rows
}

func matchItems(items []Item, query string) []matchedItem {
	matched := make([]matchedItem, 0, len(items))
	for _, item := range items {
		score := itemScore(item, query)
		children := matchItems(item.Children, query)
		if score == 0 && len(children) == 0 {
			continue
		}
		rank := score
		for _, child := range children {
			rank = max(rank, child.rank)
		}
		matched = append(matched, matchedItem{item: item, children: children, score: score, rank: rank})
	}
	slices.SortStableFunc(matched, func(left, right matchedItem) int {
		return right.rank - left.rank
	})
	return matched
}

func flattenMatches(items []matchedItem, ancestors []bool, rows *[]Row) {
	for i, matched := range items {
		hasNext := i < len(items)-1
		*rows = append(*rows, Row{
			Item: matched.item, Depth: len(ancestors), TreePrefix: treePrefix(ancestors),
			Expanded: true, Branch: len(matched.item.Children) > 0, Score: matched.score,
		})
		flattenMatches(matched.children, append(append([]bool(nil), ancestors...), hasNext), rows)
	}
}

func itemScore(item Item, query string) int {
	best := 0
	for _, field := range item.SearchFields {
		matches := fuzzy.FindNoSort(query, []string{field.Text})
		if len(matches) == 0 {
			continue
		}
		quality := min(max(matches[0].Score, 0), maxQuality)
		priority := (int(SearchSecondary) - int(field.Tier) + 1) * 1000
		best = max(best, priority+quality)
	}
	return best
}

func bestMatchCursor(rows []Row) int {
	bestIndex, bestScore := 0, 0
	for i, row := range rows {
		if row.Score > bestScore {
			bestIndex, bestScore = i, row.Score
		}
	}
	return bestIndex
}

func markAllExpanded(items []Item, expanded map[ItemID]bool) {
	for _, item := range items {
		if len(item.Children) > 0 {
			expanded[item.ID] = true
			markAllExpanded(item.Children, expanded)
		}
	}
}

func activePath(items []Item, activeID ItemID) map[ItemID]bool {
	active := make(map[ItemID]bool)
	var find func([]Item) bool
	find = func(items []Item) bool {
		for _, item := range items {
			if item.ID == activeID || find(item.Children) {
				active[item.ID] = true
				return true
			}
		}
		return false
	}
	find(items)
	return active
}

func markActivePathExpanded(items []Item, activeID ItemID, expanded map[ItemID]bool) bool {
	for _, item := range items {
		if item.ID == activeID {
			expanded[item.ID] = true
			return true
		}
		if markActivePathExpanded(item.Children, activeID, expanded) {
			expanded[item.ID] = true
			return true
		}
	}
	return false
}

func treePrefix(ancestors []bool) string {
	if len(ancestors) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, ancestorHasNext := range ancestors[:len(ancestors)-1] {
		if ancestorHasNext {
			builder.WriteString(" │  ")
		} else {
			builder.WriteString("    ")
		}
	}
	builder.WriteString(" │  ")
	return builder.String()
}

func findRow(rows []Row, id ItemID) int {
	if id == "" {
		return -1
	}
	for i, row := range rows {
		if row.Item.ID == id {
			return i
		}
	}
	return -1
}

func cloneExpanded(expanded map[ItemID]bool, valid map[ItemID]struct{}) map[ItemID]bool {
	clone := make(map[ItemID]bool, len(expanded))
	for id, value := range expanded {
		if _, exists := valid[id]; exists {
			clone[id] = value
		}
	}
	return clone
}

func clampCursor(cursor, size int) int {
	if size == 0 || cursor < 0 {
		return 0
	}
	return min(cursor, size-1)
}

func clampOffset(offset, total, height int) int {
	if total <= 0 || height <= 0 {
		return 0
	}
	return min(max(offset, 0), max(total-height, 0))
}

func ensureCursorVisible(offset, cursor, total, height int) int {
	if total <= 0 || height <= 0 {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+height {
		offset = cursor - height + 1
	}
	return clampOffset(offset, total, height)
}
