package tmux

import (
	"context"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
)

// StatusServer resolves the selected backend server with before/after checks.
func (b *TmuxBackend) StatusServer(ctx context.Context) (observe.Server, error) {
	return observe.ResolveServer(ctx, b.client)
}

// StatusSampler creates bounded inventory/capture acquisition for this server.
func (b *TmuxBackend) StatusSampler(server observe.Server) (*observe.Sampler, error) {
	return observe.NewSampler(server, b.client)
}
