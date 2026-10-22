package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/charmbracelet/x/ansi"
)

const DefaultPreviewWidth = 70

type PreviewFunc func(context.Context, list.ItemID) (string, error)

type PreviewOptions struct {
	Width int // Resolved percentage of content width.
	Read  PreviewFunc
}

type previewState struct {
	visible    bool
	width      int
	itemID     list.ItemID
	generation uint64
	ready      bool
	cancel     context.CancelFunc
	lines      []string
	err        error
}

type previewResultMsg struct {
	generation uint64
	text       string
	err        error
}

func (p *previewState) invalidate() {
	p.generation++
	p.ready, p.lines, p.err = false, nil, nil
	if p.cancel != nil {
		p.cancel()
	}
}

func handleTogglePreview(m model, _ list.ItemID) (tea.Model, tea.Cmd) {
	previousHeight := m.items.Height()
	m.preview.visible = !m.preview.visible
	m = m.reflow()
	if m.mode == modeJump && previousHeight != m.items.Height() {
		m.cancelJump()
	}
	return m, nil
}

// syncPreview coalesces selection changes into one in-flight capture.
func (m model) syncPreview() (model, tea.Cmd) {
	if !m.preview.visible && m.preview.itemID.IsZero() {
		return m, nil
	}
	var id list.ItemID
	if m.layout().previewWidth > 0 && !m.quitting && m.navigation.IsZero() {
		if row, ok := m.selectedRow(); ok {
			id = row.Item.ID
		}
	}
	p := &m.preview
	if id != p.itemID {
		p.itemID = id
		p.invalidate()
	}
	if id.IsZero() || p.ready || p.cancel != nil || m.busy || m.readPreview == nil {
		return m, nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	p.cancel = cancel
	generation, read := p.generation, m.readPreview
	return m, func() tea.Msg {
		text, err := read(ctx, id)
		return previewResultMsg{generation: generation, text: text, err: err}
	}
}

func (m model) handlePreviewResult(msg previewResultMsg) (tea.Model, tea.Cmd) {
	p := &m.preview
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if msg.generation == p.generation && !p.itemID.IsZero() {
		p.ready, p.err = true, msg.err
		if msg.err == nil {
			p.lines = terminal.ScreenLines(msg.text)
			for len(p.lines) > 0 && strings.TrimSpace(ansi.Strip(p.lines[len(p.lines)-1])) == "" {
				p.lines[len(p.lines)-1] = ""
				p.lines = p.lines[:len(p.lines)-1]
			}
		}
	}
	return m, nil
}

func (m model) viewPreview(width, height int) string {
	var lines []string
	body := m.preview.lines
	message := ""
	switch {
	case m.preview.itemID.IsZero():
		message = "no selection"
	case m.readPreview == nil:
		message = "preview unavailable"
	case m.preview.err != nil:
		message = "preview: " + terminal.Sanitize(m.preview.err.Error())
	case !m.preview.ready:
		message = "loading…"
	}
	if message != "" {
		body = []string{m.theme.meta.Render(terminal.Truncate(message, width))}
	}
	start := max(0, len(body)-max(0, height))
	for _, line := range body[start:] {
		lines = append(lines, terminal.Cut(line, 0, width))
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines, "\n"))
}
