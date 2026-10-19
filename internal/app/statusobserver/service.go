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
	server, err := resolveServiceServer(ctx)
	if err != nil {
		return ServiceStatus{}, err
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

// StartService starts or reuses the selected server's daemon without opening the
// picker. It never creates tmux; a successful daemon outlives this call.
func StartService(ctx context.Context) error {
	server, err := resolveServiceServer(ctx)
	if err != nil {
		return err
	}

	_, err = EnsureDaemon(ctx, server, "")
	return err
}

// StopService stops the observed instance for the selected tmux server without
// loading profiles or creating storage. A nil result on success means neither a
// listener nor a locked owner was observed, not that a concurrent start is excluded.
func StopService(ctx context.Context) (*daemon.Info, error) {
	server, err := resolveServiceServer(ctx)
	if err != nil {
		return nil, err
	}

	info, err := daemon.Stop(ctx, server.Identity())
	if err != nil {
		return nil, fmt.Errorf("stop status service: %w", err)
	}

	return info, nil
}

func resolveServiceServer(ctx context.Context) (observe.Server, error) {
	client, err := tmux.New()
	if err != nil {
		return observe.Server{}, err
	}

	server, err := observe.ResolveServer(ctx, client)
	if err != nil {
		return observe.Server{}, fmt.Errorf("resolve service tmux server: %w", err)
	}

	return server, nil
}
