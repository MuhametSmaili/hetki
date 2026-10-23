package list

// ResizeRows sets a line budget and a row-size function; nil uses one line per row.
// Sizes are clamped to [1, height]. The caller must render rows to fit that size.
// The function is retained until the next Resize or ResizeRows call.
func (m *Model) ResizeRows(height int, rowHeight func(Row) int) {
	m.height = max(0, height)
	m.rowHeight = rowHeight
	m.reflow()
}

func (m Model) rowsThatFit(start, direction int) int {
	if m.height == 0 || start < 0 || start >= len(m.rows) {
		return 0
	}
	if m.rowHeight == nil {
		if direction < 0 {
			return min(start+1, m.height)
		}
		return min(len(m.rows)-start, m.height)
	}

	count, remaining := 0, m.height
	for i := start; i >= 0 && i < len(m.rows); i += direction {
		height := max(1, min(m.height, m.rowHeight(m.rows[i])))
		if height > remaining {
			break
		}
		remaining -= height
		count++
	}
	return count
}

func (m *Model) reflowRows() {
	if m.height == 0 || len(m.rows) == 0 {
		m.offset = 0
		return
	}

	lastPage := len(m.rows) - m.rowsThatFit(len(m.rows)-1, -1)
	m.offset = min(max(0, m.offset), lastPage)
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.rowsThatFit(m.offset, 1) {
		m.offset = m.cursor - m.rowsThatFit(m.cursor, -1) + 1
	}
}
