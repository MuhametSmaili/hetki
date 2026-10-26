package daemon

import (
	"context"
)

// Inspection describes a single endpoint probe, not ownership of its process.
// Instance is set only when State is Running. LogPath describes the expected
// location, even when no log exists.
type Inspection struct {
	State    State
	Instance *Info
	LogPath  string
}

// Inspect probes once without waiting, creating directories, launching a
// daemon or repairing stale files. Unexpected failures remain errors rather
// than absence.
func Inspect(ctx context.Context, server string) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}

	path, err := endpoint(server)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{LogPath: logPath(path)}

	observed, err := (&Client{server: server, path: path}).probe(ctx)
	if err != nil {
		return result, err
	}

	result.State = observed.state
	if observed.state == Running {
		result.Instance = &observed.info
	}
	return result, nil
}
