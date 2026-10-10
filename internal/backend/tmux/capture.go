package tmux

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/internal/format"
)

// CapturePane reads the current screen for %N, $N:, or $N:@N.
func (b *TmuxBackend) CapturePane(ctx context.Context, target string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.HasPrefix(target, "%") {
		if err := format.ValidateObjectID("pane", target, '%'); err != nil {
			return "", err
		}
	} else {
		session, window, ok := strings.Cut(target, ":")
		if !ok {
			return "", fmt.Errorf("invalid capture target %q", target)
		}
		if err := format.ValidateObjectID("session", session, '$'); err != nil {
			return "", err
		}
		if window != "" {
			if err := format.ValidateObjectID("window", window, '@'); err != nil {
				return "", err
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	// ponytail: top 200 rows / 1 MiB; make viewport-aware if taller previews are needed.
	// Nonnegative rows exclude scrollback; omit -a to capture the active screen.
	// -p leaves paste buffers untouched.
	output, err := b.client.RunLimited(ctx, 1<<20, "capture-pane", "-p", "-e", "-N", "-S", "0", "-E", "199", "-t", target)
	if err != nil {
		return "", err
	}
	return output, nil
}
