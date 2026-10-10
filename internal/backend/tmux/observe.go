package tmux

import (
	"context"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
)

// StatusServer resolves the selected backend server with before/after checks.
func (b *TmuxBackend) StatusServer(ctx context.Context) (observe.Server, error) {
	return observe.ResolveServer(ctx, b.client)
}
