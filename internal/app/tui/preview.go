package tui

import (
	"context"
	"fmt"

	"github.com/MSmaili/hetki/internal/tui/list"
)

// Preview captures the selected item without refreshing the list.
func (a *LiveAdapter) Preview(ctx context.Context, id list.ItemID) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	item, err := a.resolveItem(id)
	if err != nil {
		return "", err
	}
	switch item.Kind {
	case liveSession, liveWindow, liveDestination:
	default:
		return "", fmt.Errorf("item %q has no pane preview", id)
	}
	b, err := a.detectBackend()
	if err != nil {
		return "", err
	}
	return b.CapturePane(ctx, item.Target)
}
