package tmux

import (
	"context"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
)

// CapturePane reads a pane occurrence or the session/window's current active pane.
func (b *TmuxBackend) CapturePane(ctx context.Context, target backend.Target) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateTarget(target); err != nil {
		return "", err
	}
	nativeTarget := target.SessionID + ":" + target.WindowID
	if target.PaneID != "" {
		nativeTarget += "." + target.PaneID
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	// Capture the visible screen through its bottom (the default end row),
	// bounded to 1 MiB. Starting at zero excludes scrollback; omitting -a
	// selects the active screen. -p leaves paste buffers untouched.
	output, err := b.client.RunLimited(ctx, 1<<20, "capture-pane", "-p", "-e", "-N", "-S", "0", "-t", nativeTarget)
	if err != nil {
		return "", err
	}
	return output, nil
}
