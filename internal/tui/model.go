package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// Run expects resolved options; its caller cancels and joins effects.
func Run(ctx context.Context, initial list.Snapshot, keys KeyMap, startMode StartMode, dispatch DispatchFunc, preview PreviewOptions, paneStatus StatusOptions) (list.ItemID, error) {
	m, err := newModelWithStartMode(initial, dispatch, keys, startMode)
	if err != nil {
		return list.ItemID{}, err
	}
	m.ctx, m.readPreview, m.preview.width = ctx, preview.Read, preview.Width
	m.paneStatus.options = paneStatus
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithFPS(120))
	final, err := p.Run()
	if err != nil {
		return list.ItemID{}, err
	}
	m, ok := final.(model)
	if !ok {
		return list.ItemID{}, fmt.Errorf("unexpected final TUI model %T", final)
	}
	return m.navigation, nil
}

type StartMode string

const (
	StartModeNormal StartMode = "normal"
	StartModeFilter StartMode = "filter"
)

func DefaultStartMode() StartMode { return StartModeFilter }

type uiMode string

const (
	modeBrowse  uiMode = "browse"
	modeFilter  uiMode = "filter"
	modeInput   uiMode = "input"
	modeConfirm uiMode = "confirm"
	modeMenu    uiMode = "menu"
)

type model struct {
	items       list.Model
	mode        uiMode
	status      string
	err         error
	busy        bool
	input       inputState
	confirm     confirmState
	menu        menuState
	pending     *ActionRequest
	pendingRows []list.ItemID
	navigation  list.ItemID
	quitting    bool
	preview     previewState
	readPreview PreviewFunc
	ctx         context.Context
	paneStatus  paneStatusState
	scope       scopeState

	width  int
	height int

	dispatch DispatchFunc
	keys     KeyMap
	theme    theme
}

func newModel(snapshot list.Snapshot, dispatch DispatchFunc) model {
	m, err := newModelWithKeys(snapshot, dispatch, DefaultKeyMap())
	if err != nil {
		return model{err: err, dispatch: dispatch, keys: DefaultKeyMap(), theme: defaultTheme()}
	}
	return m
}

func newModelWithKeys(snapshot list.Snapshot, dispatch DispatchFunc, keys KeyMap) (model, error) {
	return newModelWithStartMode(snapshot, dispatch, keys, DefaultStartMode())
}

func newModelWithStartMode(snapshot list.Snapshot, dispatch DispatchFunc, keys KeyMap, startMode StartMode) (model, error) {
	items, err := list.New(snapshot)
	if err != nil {
		return model{}, err
	}
	m := model{
		items:    items,
		dispatch: dispatch,
		keys:     keys,
		theme:    defaultTheme(),
		mode:     modeBrowse,
		ctx:      context.Background(),
		preview:  previewState{width: DefaultPreviewWidth},
	}
	m = m.reflow()
	switch startMode {
	case StartModeNormal:
	case StartModeFilter:
		if len(m.items.Rows()) > 0 {
			m.mode = modeFilter
		}
	default:
		return model{}, fmt.Errorf("invalid start mode %q", startMode)
	}
	return m, nil
}

func (m model) reflow() model {
	m.items.Resize(m.availableListHeight())
	return m
}

const headerHeight = 2

type layoutMetrics struct {
	lineWidth    int
	innerWidth   int
	listWidth    int
	previewWidth int
	headerHeight int
	middleHeight int
	compact      bool
	frameStyle   lipgloss.Style
}

func (m model) layout() layoutMetrics {
	lineWidth := m.width
	if lineWidth <= 0 {
		lineWidth = 100
	}
	height := m.height
	if height <= 0 {
		height = 24
	}
	frameStyle := responsiveFrameStyle(m.theme.appBorder, lineWidth, height)
	innerWidth := max(1, lineWidth-frameStyle.GetHorizontalFrameSize())
	listWidth, previewWidth := innerWidth, 0
	const separatorWidth, minColumnWidth = 3, 20
	if m.preview.visible && innerWidth >= 2*minColumnWidth+separatorWidth {
		available := innerWidth - separatorWidth
		previewWidth = min(max(available*m.preview.width/100, minColumnWidth), available-minColumnWidth)
		listWidth = available - previewWidth
	}
	contentHeight := height - frameStyle.GetVerticalFrameSize()
	headerRows := headerHeight
	if previewWidth > 0 && contentHeight > headerHeight+1 {
		headerRows++
	}

	return layoutMetrics{
		lineWidth:    lineWidth,
		innerWidth:   innerWidth,
		listWidth:    listWidth,
		previewWidth: previewWidth,
		headerHeight: headerRows,
		middleHeight: max(1, contentHeight-headerRows),
		compact:      listWidth < narrowListWidth,
		frameStyle:   frameStyle,
	}
}

func (m model) availableListHeight() int {
	layout := m.layout()
	if !m.stackedList(layout) {
		return layout.middleHeight
	}
	return layout.middleHeight / 2
}

// stackedList reports whether the current snapshot renders as a flat list of
// two-line rows. Flat projection items carry a session context; tree rows do
// not, so the first root item distinguishes the two layouts.
func (m model) stackedList(layout layoutMetrics) bool {
	snapshot := m.items.Snapshot()
	return len(snapshot.Items) > 0 && snapshot.Items[0].Context != "" &&
		layout.listWidth < narrowListWidth && layout.middleHeight != 1
}

func (m model) selectedRow() (list.Row, bool) { return m.items.Selected() }
