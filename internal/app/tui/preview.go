package tui

import (
	"context"
	"fmt"

	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

// Preview captures the selected item without refreshing the list.
func (a *LiveAdapter) Preview(ctx context.Context, request ui.PreviewRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	item, err := a.resolveItem(request.ItemID)
	if err != nil {
		return "", err
	}
	switch item.Kind {
	case liveSession, liveWindow, liveDestination:
	default:
		return "", fmt.Errorf("item %q has no pane preview", request.ItemID)
	}
	b, err := a.detectBackend()
	if err != nil {
		return "", err
	}
	return b.CapturePane(ctx, item.openTarget(request.PaneID))
}
