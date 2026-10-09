package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

func (m model) Init() tea.Cmd {
	if m.paneStatus.options.Read != nil {
		return statusPoll(0)
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.update(msg)
	next, previewCmd := updated.(model).syncPreview()
	next, frameCmd := next.syncStatusFrame(time.Now())
	return next, tea.Batch(cmd, previewCmd, frameCmd)
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusPollMsg:
		return m.readPaneStatus()
	case statusResultMsg:
		return m.handleStatusResult(msg)
	case statusFrameMsg:
		if msg.generation == m.paneStatus.frameGeneration {
			m.paneStatus.frameDue = time.Time{}
		}
		return m, nil
	case previewResultMsg:
		return m.handlePreviewResult(msg)
	case tea.WindowSizeMsg:
		return m.updateWindowSize(msg)
	case actionResultMsg:
		return m.handleActionResult(msg)
	case tea.PasteMsg:
		return m.updatePaste(msg)
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	default:
		return m, nil
	}
}

func (m model) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeJump {
		m.cancelJump()
	}
	m.width, m.height = msg.Width, msg.Height
	m = m.reflow()
	m.startInitialJump()
	return m, nil
}

func (m model) updatePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	switch m.mode {
	case modeFilter:
		m.items.SetQuery(m.items.Query() + singleLinePaste(msg.Content))
		return m.reflow(), nil
	case modeInput:
		m.input.Value += singleLinePaste(msg.Content)
		m.err = nil
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return handleQuit(m, "")
	}
	if m.busy {
		return m, nil
	}
	m.startInitialJump()
	if m.mode == modeMenu {
		return m.updateMenuMode(msg)
	}
	if action, ok := m.keys.Action(keyModeFor(m.mode), msg); ok {
		return m.dispatchAction(action, "")
	}
	return m.updateUnboundKey(msg)
}

func (m model) updateUnboundKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeJump:
		return m.updateJumpInput(msg)
	case modeFilter:
		if msg.Text != "" {
			m.items.SetQuery(m.items.Query() + msg.Text)
		}
		return m.reflow(), nil
	case modeInput:
		if msg.Text != "" {
			m.input.Value += msg.Text
			m.err = nil
		}
	}
	return m, nil
}
