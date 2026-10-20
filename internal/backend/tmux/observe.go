package tmux

import (
	"context"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
)

// StatusServer finds the selected tmux server and its OS start time.
func (b *TmuxBackend) StatusServer(ctx context.Context) (observe.Server, error) {
	return observe.ResolveServer(ctx, b.client)
}

// StatusSampler creates bounded inventory/capture acquisition for this server.
func (b *TmuxBackend) StatusSampler(server observe.Server) (*observe.Sampler, error) {
	return observe.NewSampler(server, b.client)
}
