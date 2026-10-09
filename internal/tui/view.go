package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MSmaili/hetki/internal/tui/list"
)

func (m model) View() tea.View {
	layout := m.layout()
	header := m.viewHeader(layout.innerWidth)
	middle := lipgloss.PlaceVertical(layout.middleHeight, lipgloss.Top, m.viewList(layout))
	if layout.previewWidth > 0 {
		left := lipgloss.NewStyle().Width(layout.listWidth).Render(middle)
		divider := m.theme.sectionLine.Render(strings.TrimSuffix(strings.Repeat(" │ \n", layout.middleHeight), "\n"))
		middle = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, m.viewPreview(layout.previewWidth, layout.middleHeight))
	}
	rendered := layout.frameStyle.Width(layout.lineWidth).Render(lipgloss.JoinVertical(lipgloss.Left, header, middle))

	if overlay := m.viewOverlay(layout.lineWidth, lipgloss.Height(rendered)); overlay != "" {
		x := max(0, (layout.lineWidth-lipgloss.Width(overlay))/2)
		y := max(0, (lipgloss.Height(rendered)-lipgloss.Height(overlay))/2)
		rendered = lipgloss.NewCompositor(
			lipgloss.NewLayer(rendered),
			lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
		).Render()
	}

	view := tea.NewView(rendered)
	view.AltScreen = true
	return view
}

func (m model) viewHeader(width int) string {
	scope := "All"
	if m.scope.tracked {
		scope = "Tracked"
	}
	return renderSearchBar(searchBarProps{
		Width: width, Filter: m.items.Query(), Hint: headerHint(m), Right: headerRight(m),
		Scope:      scope,
		RightWidth: len(fmt.Sprintf("%d/%d", len(m.items.Snapshot().Items), len(m.items.Snapshot().Items))),
		Prompt:     headerPrompt(m), Active: m.mode == modeFilter, Theme: m.theme,
	}) + "\n" + m.theme.sectionLine.Render(strings.Repeat("─", width))
}

func (m model) viewList(layout layoutMetrics) string {
	rows := m.items.VisibleRows()
	visibleRows := make([]rowProps, 0, len(rows))
	now := time.Now()
	for i, row := range rows {
		visibleRows = append(visibleRows, rowProps{
			ItemID: string(row.Item.ID), Primary: row.Item.Primary, Secondary: row.Item.Secondary,
			Trailing:   row.Item.Trailing,
			Status:     m.paneStatus.options.Bindings.StateAt(row.Item.ID, m.paneStatus.values, now),
			StatusTime: now,
			JumpLabel:  m.jumpLabel(row.Item.ID),
			Depth:      row.Depth, TreePrefix: row.TreePrefix, Expanded: row.Expanded, Branch: row.Branch,
			Active: m.items.IsActive(row.Item.ID), Selected: m.items.Offset()+i == m.items.Cursor(),
		})
	}
	return strings.Join(renderList(listProps{
		Width: layout.listWidth, EmptyText: emptyStateText(m), Rows: visibleRows,
		Compact: layout.compact, Theme: m.theme,
	}), "\n")
}

func (m model) viewOverlay(width, height int) string {
	switch m.mode {
	case modeMenu:
		return renderMenu(menuProps{
			LineWidth: width, MaxHeight: height, Title: m.menu.Title,
			Entries: m.menu.Entries, Selected: m.menu.Cursor, Hint: menuControls(m.keys), Keys: m.keys, Theme: m.theme,
		})
	case modeInput:
		prompt := m.input.Prompt
		if m.err != nil {
			prompt = m.err.Error()
		}
		return renderInputModal(inputModalProps{
			LineWidth: width, Title: m.input.Title, Prompt: prompt, Value: m.input.Value,
			Hint: modalControls(m.keys, KeyModeInput, "submit"), Theme: m.theme,
		})
	case modeConfirm:
		return renderConfirmModal(confirmModalProps{
			LineWidth: width, Title: m.confirm.Title, Body: m.confirm.Body,
			Hint: modalControls(m.keys, KeyModeConfirm, "confirm"), Theme: m.theme,
		})
	default:
		return ""
	}
}

func (m model) jumpLabel(id list.ItemID) string {
	if m.mode != modeJump {
		return ""
	}
	return m.jump.labelFor(id)
}

func modalControls(keys KeyMap, mode KeyMode, confirmLabel string) string {
	parts := make([]string, 0, 2)
	for _, control := range []struct {
		action ActionID
		label  string
	}{
		{action: ActionConfirm, label: confirmLabel},
		{action: ActionCancel, label: "cancel"},
	} {
		if bound := keys.Keys(mode, control.action); len(bound) > 0 {
			parts = append(parts, strings.Join(bound, "/")+" "+control.label)
		}
	}
	return strings.Join(parts, " | ")
}

func menuControls(keys KeyMap) string {
	up := keys.Keys(KeyModeMenu, ActionMoveUp)
	down := keys.Keys(KeyModeMenu, ActionMoveDown)
	confirm := keys.Keys(KeyModeMenu, ActionConfirm)
	cancel := keys.Keys(KeyModeMenu, ActionCancel)
	if len(up) == 0 || len(down) == 0 || len(confirm) == 0 || len(cancel) == 0 {
		return ""
	}
	return up[0] + "/" + down[0] + " move | " + confirm[0] + " select | " + cancel[0] + " cancel"
}

func headerPrompt(m model) string {
	switch m.mode {
	case modeBrowse:
		return " NORMAL "
	case modeFilter:
		return " FILTER "
	case modeJump:
		return " JUMP "
	default:
		return ""
	}
}

func headerHint(m model) string {
	if m.mode == modeJump {
		hint := "type a label to jump"
		if bound := m.keys.Keys(KeyModeJump, ActionFilter); len(bound) > 0 {
			hint += " or " + bound[0] + " to filter"
		}
		return hint
	}
	if m.mode != modeBrowse {
		return ""
	}
	var parts []string
	for _, control := range []struct {
		action ActionID
		label  string
	}{
		{ActionFilter, "filter"},
		{ActionJump, "jump"},
	} {
		if bound := m.keys.Keys(KeyModeNormal, control.action); len(bound) > 0 {
			parts = append(parts, bound[0]+" to "+control.label)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "press " + strings.Join(parts, " or ")
}

func headerRight(m model) string {
	if m.err != nil {
		return m.err.Error()
	}
	if m.busy {
		return m.status
	}
	if notice := m.items.Snapshot().Notice; notice != "" {
		return notice
	}
	query := m.items.Query()
	if m.scope.tracked {
		query = "tracked"
	}
	return rootCountLabel(m.items.Snapshot(), m.items.ShownRoots(), query)
}

func rootCountLabel(snapshot list.Snapshot, shown int, query string) string {
	total := len(snapshot.Items)
	if total == 0 {
		return ""
	}
	if strings.TrimSpace(query) == "" {
		return fmt.Sprintf("%d", total)
	}
	return fmt.Sprintf("%d/%d", shown, total)
}

func responsiveFrameStyle(base lipgloss.Style, width, height int) lipgloss.Style {
	if width < 4 {
		return lipgloss.NewStyle()
	}
	if height < 5 {
		base = base.BorderTop(false).BorderBottom(false)
	}
	if width < 32 {
		return base.Padding(0, 0)
	}
	if width < 72 {
		return base.Padding(0, 1)
	}
	return base
}

func emptyStateText(m model) string {
	if m.scope.tracked {
		return "no matching tracked items"
	}
	if strings.TrimSpace(m.items.Query()) != "" {
		return "no matching items"
	}
	return "no sessions found"
}
