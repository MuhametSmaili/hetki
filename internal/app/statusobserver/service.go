package statusobserver

import (
	"context"
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
)

// ServiceStatus identifies the selected tmux server and its daemon probe.
type ServiceStatus struct {
	Server observe.Server
	Daemon daemon.Inspection
}

// InspectService uses tmux's normal server selection without starting a server,
// loading picker inventory or launching a daemon.
func InspectService(ctx context.Context) (ServiceStatus, error) {
	client, err := tmux.New()
	if err != nil {
		return ServiceStatus{}, err
	}

	server, err := observe.ResolveServer(ctx, client)
	if err != nil {
		return ServiceStatus{}, fmt.Errorf("resolve service tmux server: %w", err)
	}

	inspection, err := daemon.Inspect(ctx, server.Identity())
	if err != nil {
		return ServiceStatus{}, fmt.Errorf("inspect status service: %w", err)
	}

	if err := server.Validate(ctx); err != nil {
		return ServiceStatus{}, fmt.Errorf("recheck service tmux server: %w", err)
	}

	return ServiceStatus{Server: server, Daemon: inspection}, nil
}
