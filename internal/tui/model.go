package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// Run expects resolved options; its caller cancels and joins effects.
// Cancellation quits the program and returns the context's error.
func Run(ctx context.Context, initial list.Snapshot, options Options, runtime Runtime) (Result, error) {
	m, err := newModelWithOptions(ctx, initial, options, runtime)
	if err != nil {
		return Result{}, err
	}
	p := tea.NewProgram(m, tea.WithFPS(120))

	// Quit instead of tea.WithContext: a cancelled context kills the program,
	// which closes the terminal input while its reader goroutine still uses it.
	// Quit waits for that goroutine.
	stopQuit := context.AfterFunc(ctx, p.Quit)
	final, err := p.Run()
	stopQuit()

	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	m, ok := final.(model)
	if !ok {
		return Result{}, fmt.Errorf("unexpected final TUI model %T", final)
	}
	return m.result(), nil
}

func (m model) result() Result {
	return Result{
		Navigation: m.navigation,
		State:      State{View: m.view, Scope: m.scope.mode, Preview: m.preview.enabled},
	}
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
	view        ViewMode
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

	width        int
	height       int
	inline       bool
	pickerHeight Height
	border       BorderMode

	dispatch DispatchFunc
	keys     KeyMap
	theme    theme
}

func newModelWithOptions(ctx context.Context, snapshot list.Snapshot, options Options, runtime Runtime) (model, error) {
	if err := options.Validate(); err != nil {
		return model{}, err
	}
	items, err := list.New(snapshot)
	if err != nil {
		return model{}, err
	}
	m := model{
		items:        items,
		view:         options.Initial.View,
		dispatch:     runtime.Dispatch,
		keys:         options.Keys,
		theme:        defaultTheme(),
		mode:         modeBrowse,
		ctx:          ctx,
		preview:      previewState{enabled: options.Initial.Preview, width: options.PreviewWidth},
		scope:        scopeState{mode: options.Initial.Scope},
		readPreview:  runtime.Preview,
		inline:       !options.Height.Fullscreen(),
		pickerHeight: options.Height,
		border:       options.Border,
		paneStatus:   paneStatusState{options: runtime.Status},
	}
	m.applyScope("", time.Now())
	if options.StartMode == StartModeFilter && (len(m.items.Rows()) > 0 || m.scope.mode == ScopeTracked) {
		m.mode = modeFilter
	}
	return m.reflow(), nil
}

func (m model) reflow() model {
	layout := m.layout()
	if layout.listWidth < narrowListWidth && layout.middleHeight > 1 {
		m.items.ResizeRows(layout.middleHeight, stackedRowHeight)
	} else {
		m.items.Resize(layout.middleHeight)
	}
	return m
}

func stackedRowHeight(row list.Row) int {
	if row.Item.Context != "" || row.Depth > 0 {
		return 2
	}
	return 1
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
	height := m.displayHeight()
	frame := m.theme.appBorder
	if m.border == BorderNone {
		frame = lipgloss.NewStyle()
	}
	frameStyle := responsiveFrameStyle(frame, lineWidth, height)
	innerWidth := max(1, lineWidth-frameStyle.GetHorizontalFrameSize())
	listWidth, previewWidth := innerWidth, 0
	const separatorWidth, minColumnWidth = 3, 20
	if m.preview.enabled && innerWidth >= 2*minColumnWidth+separatorWidth {
		available := innerWidth - separatorWidth
		previewWidth = min(max(available*m.preview.width/100, minColumnWidth), available-minColumnWidth)
		listWidth = available - previewWidth
	}
	contentHeight := height - frameStyle.GetVerticalFrameSize()
	headerRows := headerHeight
	if previewWidth > 0 && contentHeight > headerHeight+1 {
		headerRows++
	}
	if m.inline {
		headerRows = min(headerRows, max(0, contentHeight-1))
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

func (m model) selectedRow() (list.Row, bool) { return m.items.Selected() }
