package tui

import (
	"context"
	"fmt"
	"sync"

	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/frecency"
	ui "github.com/MSmaili/hetki/internal/tui"
	"github.com/MSmaili/hetki/internal/tui/list"
)

type LiveAdapter struct {
	DetectBackend func(...string) (backend.Backend, error)
	mu            sync.RWMutex // Protects the index and backend instance; never held during capture.
	cached        backend.Backend
	index         itemIndex
	statusServer  observe.Server
	statusErr     error
	projection    projectionKind
	frecency      *frecency.Store
	frecencyErr   error
	pendingRecord *navigationRecord
}

func NewLiveAdapter(detectBackend func(...string) (backend.Backend, error)) *LiveAdapter {
	store, err := frecency.DefaultStore()
	return newLiveAdapter(detectBackend, store, err)
}

func newLiveAdapter(detectBackend func(...string) (backend.Backend, error), store *frecency.Store, storeErr error) *LiveAdapter {
	return &LiveAdapter{DetectBackend: detectBackend, projection: projectionFlat, frecency: store, frecencyErr: storeErr}
}

func (a *LiveAdapter) Execute(ctx context.Context, request ui.ActionRequest) (ui.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ui.ActionResult{}, err
	}
	if request.ActionID == ui.ActionToggleProjection {
		return a.toggleProjection(ctx, request.ItemID)
	}
	var item liveItem
	if !request.ItemID.IsZero() {
		var err error
		item, err = a.resolveItem(request.ItemID)
		if err != nil {
			return ui.ActionResult{}, err
		}
	}
	switch request.ActionID {
	case ui.ActionLastSession:
		return a.openLastSession(ctx)
	case ui.ActionRefresh:
		snapshot, err := a.loadSnapshot(ctx)
		return ui.ActionResult{Message: "refreshed", Snapshot: &snapshot}, err
	case ui.ActionCreateSession:
		return a.createSession(ctx, request)
	default:
		return a.executeItem(ctx, request, item)
	}
}

func (a *LiveAdapter) openLastSession(ctx context.Context) (ui.ActionResult, error) {
	a.mu.RLock()
	var last liveItem
	for _, item := range a.index {
		if item.Last {
			last = item
			break
		}
	}
	a.mu.RUnlock()
	if last.ID.IsZero() {
		return ui.ActionResult{}, fmt.Errorf("no previous session available")
	}
	return a.openItem(ctx, last)
}

func (a *LiveAdapter) executeItem(ctx context.Context, request ui.ActionRequest, item liveItem) (ui.ActionResult, error) {
	if item.ID.IsZero() {
		return ui.ActionResult{}, fmt.Errorf("action requires a selected item")
	}
	if (request.ActionID == ui.ActionRename || request.ActionID == ui.ActionDelete) &&
		item.Kind != liveWindow && item.Kind != liveDestination {
		return ui.ActionResult{}, fmt.Errorf("item %q does not support a window action", item.ID)
	}
	switch request.ActionID {
	case ui.ActionContextMenu:
		menu, err := contextualMenu(item)
		if err != nil {
			return ui.ActionResult{}, err
		}
		return ui.ActionResult{Menu: &menu}, nil
	case ui.ActionOpen:
		return a.openItem(ctx, item)
	case ui.ActionCreateWindow:
		return a.createWindow(ctx, request, item)
	case ui.ActionRename:
		return a.renameItem(ctx, request, item)
	case ui.ActionRenameSession:
		session, err := owningSessionItem(item)
		if err != nil {
			return ui.ActionResult{}, err
		}
		return a.renameItem(ctx, request, session)
	case ui.ActionDelete:
		return a.deleteItem(ctx, request, item)
	case ui.ActionDeleteSession:
		session, err := owningSessionItem(item)
		if err != nil {
			return ui.ActionResult{}, err
		}
		return a.deleteItem(ctx, request, session)
	default:
		return ui.ActionResult{}, fmt.Errorf("action %q is not implemented", request.ActionID)
	}
}

func (a *LiveAdapter) resolveItem(id list.ItemID) (liveItem, error) {
	if id.IsZero() {
		return liveItem{}, fmt.Errorf("action requires a selected item")
	}
	a.mu.RLock()
	item, exists := a.index[id]
	a.mu.RUnlock()
	if !exists {
		return liveItem{}, fmt.Errorf("selected item %q is stale", id)
	}
	return item, nil
}

func (a *LiveAdapter) detectBackend() (backend.Backend, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached != nil {
		return a.cached, nil
	}
	var b backend.Backend
	var err error
	if a.DetectBackend != nil {
		b, err = a.DetectBackend()
	} else {
		b, err = backend.Detect()
	}
	if err != nil {
		return nil, err
	}
	a.cached = b
	return b, nil
}
