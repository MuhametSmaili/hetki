package daemon

import (
	"context"
	"errors"
	"os"
)

// Inspection describes a single endpoint probe, not ownership of its process.
// Instance is nil when no endpoint listens; startup or shutdown may be in progress.
// LogPath describes the expected location, even when no log exists.
type Inspection struct {
	Instance *Info
	LogPath  string
}

// Inspect probes without creating directories, launching a daemon or repairing
// stale files. Unexpected failures remain errors rather than absence.
func Inspect(ctx context.Context, server string) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}

	path, err := endpoint(server)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{LogPath: logPath(path)}

	if err := validateEndpointDirectory(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return result, err
	}

	client := &Client{server: server, path: path}
	info, err := client.Ping(ctx)
	if isMissingEndpoint(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}

	result.Instance = &info
	return result, nil
}
