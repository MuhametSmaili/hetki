// Package daemon owns the process lifetime and private snapshot transport
// for one backend server incarnation. Composition supplies the bound source.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/MSmaili/hetki/internal/status"
)

type Binding struct {
	Server string

	// Alive validates this exact server incarnation and honors cancellation.
	// It may be called concurrently by the watcher and request handlers.
	Alive func(context.Context) bool
}

// Source reads complete cached snapshots. Close joins its workers after the
// daemon cancels the context passed to start; concurrent reads must honor cancellation.
type Source interface {
	ReadStatus(context.Context) (status.Snapshot, error)
	Close() error
}

// SourceFactory creates the daemon-owned source. Report writes bounded,
// rate-limited source failures to the daemon log and is safe for concurrent use.
// The source must join every caller of report before Close returns.
type SourceFactory func(ctx context.Context, report func(error)) (Source, error)

func Serve(ctx context.Context, binding Binding, start SourceFactory) error {
	if binding.Alive == nil || start == nil {
		return fmt.Errorf("status daemon requires lifetime check and source factory")
	}

	c, err := Connect(binding.Server)
	if err != nil {
		return err
	}

	release, err := lockEndpoint(c.path)
	if err != nil || release == nil {
		return err
	}
	defer release()

	logs, err := openDaemonLog(c.LogPath())
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer logs.close()
	defer logs.recordPanic()

	logs.info("starting")
	err = serveLocked(ctx, c.path, binding, start, logs)
	if err != nil {
		logs.failure("serve_failed", err)
	} else {
		logs.info("stopped")
	}

	return err
}

func serveLocked(ctx context.Context, path string, binding Binding, start SourceFactory, logs *daemonLog) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if !bindingAlive(ctx, binding) {
		return fmt.Errorf("status server no longer lives")
	}

	if err := removeStaleEndpoint(ctx, path); err != nil {
		return err
	}

	source, err := start(ctx, func(err error) {
		if err != nil {
			logs.repeatedFailure("source_failed", err)
		}
	})
	if err != nil {
		return err
	}

	if source == nil {
		return fmt.Errorf("status source factory returned nil")
	}
	defer func() {
		cancel()
		err = errors.Join(err, source.Close())
	}()

	if !bindingAlive(ctx, binding) {
		return fmt.Errorf("status server changed during startup")
	}

	listener, cleanup, err := listen(path)
	if err != nil {
		return err
	}
	defer cleanup()

	var epoch [16]byte
	if _, err := rand.Read(epoch[:]); err != nil {
		return err
	}

	d := daemon{
		binding: binding,
		source:  source,
		cancel:  cancel,
		logs:    logs,
		info: Info{
			PID:   os.Getpid(),
			Epoch: hex.EncodeToString(epoch[:]),
		},
	}
	logs.ready(d.info.Epoch)

	return d.run(ctx, listener)
}

func bindingAlive(ctx context.Context, binding Binding) bool {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	return ctx.Err() == nil && binding.Alive(ctx) && ctx.Err() == nil
}
