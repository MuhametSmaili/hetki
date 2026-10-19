package statusobserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MuhametSmaili/hetki/internal/status/daemon"
)

// DaemonCommand names the private entry point shared by registration and launch.
const DaemonCommand = "__status-daemon"

const maxServerDescriptorBytes = 8192

// EnsureDaemon starts or reuses the daemon for this exact server incarnation.
// The detached child receives a descriptor rather than inheriting TMUX selection.
// Executable may be empty to launch the current binary.
func EnsureDaemon(ctx context.Context, server observe.Server, executable string) (*daemon.Client, error) {
	if err := server.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate status daemon server: %w", err)
	}

	identity := server.Identity()
	return daemon.Ensure(ctx, identity, executable, func(ctx context.Context) ([]string, error) {
		// Keep profile context in the caller's error until child startup can
		// return an equally useful diagnostic instead of just an exit status.
		if _, err := loadProfiles(ctx); err != nil {
			return nil, err
		}

		descriptor, err := json.Marshal(server)
		if err != nil {
			return nil, fmt.Errorf("encode status daemon server: %w", err)
		}

		if len(descriptor) > maxServerDescriptorBytes {
			return nil, fmt.Errorf("status daemon server descriptor exceeds limit")
		}

		return []string{DaemonCommand, identity, string(descriptor)}, nil
	})
}

// ServeDaemon is the executable's private startup entry point. Native liveness,
// sampling and subscriptions remain pinned to the supplied server incarnation.
func ServeDaemon(ctx context.Context, identity, descriptor string) error {
	server, err := decodeServer(identity, descriptor)
	if err != nil {
		return err
	}

	binding := daemon.Binding{
		Server: identity,
		Alive: func(ctx context.Context) bool {
			return server.Validate(ctx) == nil
		},
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var stopSourceWatch func() bool
	defer func() {
		if stopSourceWatch != nil {
			stopSourceWatch()
		}
	}()

	return daemon.Serve(ctx, binding, func(ctx context.Context, report func(error)) (daemon.Source, error) {
		source, err := startDaemonSource(ctx, server, report)
		if err != nil {
			return nil, err
		}

		// A terminal acquisition failure must not leave a healthy-looking daemon.
		stopSourceWatch = context.AfterFunc(source.ctx, cancel)
		return source, nil
	})
}

func decodeServer(identity, descriptor string) (observe.Server, error) {
	if len(descriptor) > maxServerDescriptorBytes {
		return observe.Server{}, fmt.Errorf("status daemon server descriptor exceeds limit")
	}

	var server observe.Server
	if err := json.Unmarshal([]byte(descriptor), &server); err != nil {
		return observe.Server{}, fmt.Errorf("decode status daemon server: %w", err)
	}

	if server.Identity() != identity {
		return observe.Server{}, fmt.Errorf("%w: status daemon identity differs from descriptor", observe.ErrServerChanged)
	}

	return server, nil
}

func startDaemonSource(ctx context.Context, server observe.Server, report func(error)) (*Source, error) {
	catalog, err := loadProfiles(ctx)
	if err != nil {
		return nil, err
	}

	client, err := tmux.New()
	if err != nil {
		return nil, fmt.Errorf("create status tmux client: %w", err)
	}

	sampler, err := observe.NewSampler(server, client)
	if err != nil {
		return nil, fmt.Errorf("create status sampler: %w", err)
	}

	return NewSource(ctx, server, sampler, profileSelector(catalog), report)
}
