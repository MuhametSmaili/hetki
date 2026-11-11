package list

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/search"
)

type Row struct {
	Item       Item
	Depth      int
	TreePrefix string
	Expanded   bool
	Branch     bool
	Score      int
}

type Model struct {
	snapshot      Snapshot
	rows          []rowRef
	cursor        int
	offset        int
	height        int
	rowHeight     func(Row) int
	shownRoot     int
	query         string
	included      map[ItemID]struct{}
	details       map[ItemID]Details
	search        *search.Index
	projection    *itemProjection
	expanded      map[ItemID]bool // Copy-on-write: preparation borrows this map.
	active        map[ItemID]bool
	deferred      bool
	pendingSearch bool
	searchKey     SearchKey
	preferred     []ItemID
	workContext   context.Context
}

// New borrows an immutable snapshot. Use Replace to publish changed items.
func New(snapshot Snapshot) (Model, error) {
	var m Model
	err := m.Replace(snapshot, ItemID{})
	return m, err
}

// Snapshot returns the borrowed snapshot; callers must not mutate its contents.
func (m Model) Snapshot() Snapshot { return m.snapshot }

// Rows materializes all current rows into a caller-owned slice. Nested item
// slices remain borrowed and immutable. Rendering should use VisibleRows instead.
func (m Model) Rows() []Row { return materializeRows(m.rows) }

// Len is the row count after scope, filtering and expansion are applied.
func (m Model) Len() int { return len(m.rows) }

// RowIDs returns the current row order without materializing presentation data.
func (m Model) RowIDs() []ItemID {
	ids := make([]ItemID, len(m.rows))
	for i, row := range m.rows {
		ids[i] = row.item.ID
	}
	return ids
}

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
	return m.rows[m.cursor].materialize(), true
}

// VisibleRows materializes only the viewport. Its ownership is the same as Rows.
func (m Model) VisibleRows() []Row {
	start := m.offset
	end := start + m.rowsThatFit(start, 1)
	if start < 0 || start >= end {
		return nil
	}
	return materializeRows(m.rows[start:end])
}

// Replace borrows a new immutable snapshot and retains surviving selection and expansion.
func (m *Model) Replace(snapshot Snapshot, preferred ItemID) error {
	ctx := m.operationContext()
	ids, err := validateSnapshot(ctx, snapshot)
	if err != nil {
		return err
	}
	selectedID := preferred
	if selectedID.IsZero() {
		if selected, ok := m.Selected(); ok {
			selectedID = selected.Item.ID
		}
	}
	candidate := *m
	if candidate.searchKey.owner == nil {
		candidate.searchKey.owner = new(byte)
	}
	candidate.snapshot = snapshot
	candidate.details = nil
	candidate.invalidateSearch()
	candidate.expanded = cloneExpanded(ctx, m.expanded, ids)
	markAllExpanded(ctx, snapshot.Items, candidate.expanded)
	restoreBranchExpansion(ctx, m.snapshot.Items, m.expanded, candidate.expanded)

	candidate.active = activePath(ctx, snapshot.Items, snapshot.ActiveItemID)
	markActivePathExpanded(ctx, snapshot.Items, snapshot.ActiveItemID, candidate.expanded)
	candidate.applyFilter()
	candidate.PreferSelection(selectedID)
	if idx := findRow(candidate.rows, selectedID); idx >= 0 {
		candidate.cursor = idx
	}
	candidate.reflow()
	if err := ctx.Err(); err != nil {
		return err
	}
	*m = candidate
	return nil
}

func (m *Model) SetQuery(query string) {
	m.SetFilter(query, nil)
}

// SetFilterQuery changes the query while retaining the current inclusion scope.
func (m *Model) SetFilterQuery(query string) {
	if m.query == query {
		return
	}
	m.query = query
	m.preferred = nil
	m.applyFilter()
	m.reflow()
}

// SetFilter retains ancestors of included items; nil allows all items.
func (m *Model) SetFilter(query string, included map[ItemID]struct{}) {
	if m.query == query && (m.included == nil) == (included == nil) && maps.Equal(m.included, included) {
		return
	}
	selected, hasSelection := m.Selected()
	preserve := m.query == query
	if !preserve {
		m.preferred = nil
	}
	m.query = query
	if (m.included == nil) != (included == nil) || !maps.Equal(m.included, included) {
		m.included = maps.Clone(included)
		m.invalidateSearch()
	}
	m.applyFilter()
	if preserve && hasSelection {
		m.PreferSelection(selected.Item.ID)
	}
	m.reflow()
}

func (m *Model) Resize(height int) {
	m.ResizeRows(height, nil)
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
	if m.rowHeight == nil {
		m.Move(delta * m.height)
		return
	}

	direction := 1
	if delta < 0 {
		direction = -1
	}
	m.Move(delta * m.rowsThatFit(m.cursor, direction))
}

func (m *Model) Select(id ItemID) bool {
	idx := findRow(m.rows, id)
	if idx < 0 {
		return false
	}
	m.cursor = idx
	if m.pendingSearch {
		m.preferred = []ItemID{id}
	}
	m.reflow()
	return true
}

func (m *Model) SelectSurvivor(previous []ItemID, anchor ItemID) bool {
	position := slices.Index(previous, anchor)
	if position < 0 {
		return false
	}
	//nolint:modernize // Keep selection changes explicit rather than hiding them in a predicate.
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
	m.expanded = maps.Clone(m.expanded)
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
	m.expanded = maps.Clone(m.expanded)
	markAllExpanded(m.operationContext(), m.snapshot.Items, m.expanded)
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
		for _, candidate := range slices.Backward(indices) {
			if candidate < m.cursor {
				next = candidate
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
	m.searchKey.query++
	m.pendingSearch = m.deferred && strings.TrimSpace(m.query) != ""
	if m.pendingSearch {
		return
	}
	ctx := m.operationContext()
	if ctx.Err() != nil {
		return
	}
	var selectedID ItemID
	if selected, ok := m.Selected(); ok {
		selectedID = selected.Item.ID
	}
	if m.projection == nil {
		m.projection = &itemProjection{items: projectItems(ctx, m.snapshot.Items, m.included, m.details)}
	}
	items := m.projection.items
	if strings.TrimSpace(m.query) == "" {
		m.rows = flatten(ctx, items, m.expanded, false)
		if idx := findRow(m.rows, selectedID); idx >= 0 {
			m.cursor = idx
		} else {
			m.cursor = clampCursor(m.cursor, len(m.rows))
		}
	} else {
		rows, err := m.matchingRows(ctx, items)
		if err != nil {
			return
		}
		m.rows = rows
		m.cursor = bestMatchCursor(m.rows)
	}
	m.shownRoot = 0
	for _, row := range m.rows {
		if row.depth == 0 {
			m.shownRoot++
		}
	}
}

func (m *Model) matchingRows(ctx context.Context, items []Item) ([]rowRef, error) {
	if m.search == nil {
		m.search = prepareSearch(ctx, items)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scores, err := m.search.ScoresContext(ctx, m.query)
	if err != nil {
		return nil, err
	}
	return rankedMatches(ctx, items, scores), ctx.Err()
}

func (m *Model) reflow() {
	m.cursor = clampCursor(m.cursor, len(m.rows))
	if m.rowHeight != nil {
		m.reflowRows()
		return
	}

	m.offset = clampOffset(m.offset, len(m.rows), m.height)
	m.offset = ensureCursorVisible(m.offset, m.cursor, len(m.rows), m.height)
}

func (m Model) matchIndices() []int {
	indices := make([]int, 0, len(m.rows))
	for i, row := range m.rows {
		if row.score > 0 {
			indices = append(indices, i)
		}
	}
	return indices
}

func flatten(ctx context.Context, items []Item, expanded map[ItemID]bool, includeAll bool) []rowRef {
	rows := make([]rowRef, 0, visibleItemCount(ctx, items, expanded, includeAll))
	var ancestors [MaxDepth]bool
	flattenAtDepth(ctx, items, ancestors[:0], expanded, includeAll, &rows)
	return rows
}

func visibleItemCount(ctx context.Context, items []Item, expanded map[ItemID]bool, includeAll bool) int {
	count := len(items)
	for i, item := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return 0
		}
		if len(item.Children) > 0 && (includeAll || expanded[item.ID]) {
			count += visibleItemCount(ctx, item.Children, expanded, includeAll)
		}
	}
	return count
}

func flattenAtDepth(ctx context.Context, items []Item, ancestors []bool, expanded map[ItemID]bool, includeAll bool, rows *[]rowRef) {
	for i := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return
		}
		item := &items[i]
		hasNext := i < len(items)-1
		isExpanded := includeAll || expanded[item.ID]
		*rows = append(*rows, newRowRef(item, ancestors, hasNext, isExpanded, 0))
		if len(item.Children) > 0 && isExpanded {
			flattenAtDepth(ctx, item.Children, append(ancestors, hasNext), expanded, includeAll, rows)
		}
	}
}

type matchedItem struct {
	item     *Item
	children []matchedItem
	score    int
	rank     int
}

func rankedMatches(ctx context.Context, items []Item, scores []int) []rowRef {
	scoreIndex := 0
	matched, count := matchItems(ctx, items, scores, &scoreIndex)
	rows := make([]rowRef, 0, count)
	var ancestors [MaxDepth]bool
	flattenMatches(ctx, matched, ancestors[:0], &rows)
	return rows
}

func matchItems(ctx context.Context, items []Item, scores []int, scoreIndex *int) ([]matchedItem, int) {
	if len(items) == 0 {
		return nil, 0
	}
	matched := make([]matchedItem, 0, len(items))
	count := 0
	for i := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return nil, 0
		}
		item := &items[i]
		score := scores[*scoreIndex]
		(*scoreIndex)++

		children, childCount := matchItems(ctx, item.Children, scores, scoreIndex)
		if score == 0 && len(children) == 0 {
			continue
		}
		rank := branchRank(ctx, score, children)
		matched = append(matched, matchedItem{item: item, children: children, score: score, rank: rank})
		count += 1 + childCount
	}
	sortMatches(ctx, matched)
	return matched, count
}

func branchRank(ctx context.Context, score int, children []matchedItem) int {
	for i, child := range children {
		if i%64 == 0 && ctx.Err() != nil {
			return 0
		}
		score = max(score, child.rank)
	}
	return score
}

func flattenMatches(ctx context.Context, items []matchedItem, ancestors []bool, rows *[]rowRef) {
	for i, matched := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return
		}
		hasNext := i < len(items)-1
		*rows = append(*rows, newRowRef(matched.item, ancestors, hasNext, true, matched.score))
		if len(matched.children) > 0 {
			flattenMatches(ctx, matched.children, append(ancestors, hasNext), rows)
		}
	}
}

// Search entry positions follow item preorder, including entries with no fields.
// matchItems consumes scores in that same order.
func prepareSearch(ctx context.Context, items []Item) *search.Index {
	entries := make([][]search.Field, 0, visibleItemCount(ctx, items, nil, true))
	var collect func([]Item)
	collect = func(items []Item) {
		for i := range items {
			if i%64 == 0 && ctx.Err() != nil {
				return
			}
			entries = append(entries, items[i].SearchFields)
			collect(items[i].Children)
		}
	}
	collect(items)
	index, _ := search.PrepareContext(ctx, entries)
	return index
}

func bestMatchCursor(rows []rowRef) int {
	bestIndex, bestScore := 0, 0
	for i, row := range rows {
		if row.score > bestScore {
			bestIndex, bestScore = i, row.score
		}
	}
	return bestIndex
}

func markAllExpanded(ctx context.Context, items []Item, expanded map[ItemID]bool) {
	for i, item := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return
		}
		if len(item.Children) > 0 {
			expanded[item.ID] = true
			markAllExpanded(ctx, item.Children, expanded)
		}
	}
}

func restoreBranchExpansion(ctx context.Context, items []Item, previous, expanded map[ItemID]bool) {
	for i, item := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return
		}
		if len(item.Children) == 0 {
			continue
		}
		if _, exists := expanded[item.ID]; exists {
			expanded[item.ID] = previous[item.ID]
		}
		restoreBranchExpansion(ctx, item.Children, previous, expanded)
	}
}

func activePath(ctx context.Context, items []Item, activeID ItemID) map[ItemID]bool {
	active := make(map[ItemID]bool)
	var find func([]Item) bool
	find = func(items []Item) bool {
		for i, item := range items {
			if i%64 == 0 && ctx.Err() != nil {
				return false
			}
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

func markActivePathExpanded(ctx context.Context, items []Item, activeID ItemID, expanded map[ItemID]bool) bool {
	for i, item := range items {
		if i%64 == 0 && ctx.Err() != nil {
			return false
		}
		if item.ID == activeID {
			expanded[item.ID] = true
			return true
		}
		if markActivePathExpanded(ctx, item.Children, activeID, expanded) {
			expanded[item.ID] = true
			return true
		}
	}
	return false
}

func treePrefix(ancestors []bool, hasNext bool) string {
	if len(ancestors) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, ancestorHasNext := range ancestors[1:] {
		if ancestorHasNext {
			builder.WriteString("│  ")
		} else {
			builder.WriteString("   ")
		}
	}
	if hasNext {
		builder.WriteString("├─ ")
	} else {
		builder.WriteString("└─ ")
	}
	return builder.String()
}

func findRow(rows []rowRef, id ItemID) int {
	if id.IsZero() {
		return -1
	}
	for i, row := range rows {
		if row.item.ID == id {
			return i
		}
	}
	return -1
}

func cloneExpanded(ctx context.Context, expanded map[ItemID]bool, valid map[ItemID]struct{}) map[ItemID]bool {
	clone := make(map[ItemID]bool, len(expanded))
	for id, value := range expanded {
		if ctx.Err() != nil {
			return nil
		}
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
