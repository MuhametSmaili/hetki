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
	"time"
)

const livenessTimeout = 2 * time.Second

type Binding struct {
	Server string

	// Alive validates this exact server incarnation and honors cancellation.
	// It may be called concurrently by the watcher and request handlers.
	Alive func(context.Context) bool
}

// Source supplies the cached view that the daemon pushes to subscribers.
// Changes returns a channel that closes at the next change after the call. Get
// the channel before ReadView, so that no change is lost. ReadView must stop on
// cancellation and must not do I/O. Close joins the source workers after the
// daemon cancels the context passed to start.
type Source interface {
	Changes() <-chan struct{}
	ReadView(context.Context) (View, error)
	Close() error
}

// SourceFactory creates the daemon-owned source. Report writes bounded,
// rate-limited source failures to the daemon log and is safe for concurrent use.
// The source must join every caller of report before Close returns.
type SourceFactory func(ctx context.Context, report func(error)) (Source, error)

func Serve(ctx context.Context, binding Binding, start SourceFactory) (err error) {
	if binding.Alive == nil || start == nil {
		return fmt.Errorf("status daemon requires lifetime check and source factory")
	}

	path, err := endpoint(binding.Server)
	if err != nil {
		return err
	}

	if err := prepareEndpoint(path); err != nil {
		return err
	}

	lock, err := lockEndpoint(path)
	if err != nil || lock == nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	epoch := hex.EncodeToString(random[:])
	if err := lock.setOwner(epoch); err != nil {
		return fmt.Errorf("record daemon lock owner: %w", err)
	}

	logs, err := openDaemonLog(logPath(path))
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer logs.close()
	defer logs.recordPanic()

	logs.info("starting")
	err = serveLocked(ctx, path, epoch, binding, start, logs)
	if err != nil {
		logs.failure("serve_failed", err)
	} else {
		logs.info("stopped")
	}

	return err
}

func serveLocked(ctx context.Context, path, epoch string, binding Binding, start SourceFactory, logs *daemonLog) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	executable, executableErr := currentExecutable()
	if executableErr != nil {
		logs.failure("executable_check_unavailable", executableErr)
	}

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

	d := daemon{
		binding:     binding,
		source:      source,
		cancel:      cancel,
		logs:        logs,
		subscribers: make(chan struct{}, maxSubscribers),
		executable:  executable,
		info: Info{
			PID:   os.Getpid(),
			Epoch: epoch,
		},
	}
	logs.ready(d.info.Epoch)

	return d.run(ctx, listener)
}

func bindingAlive(ctx context.Context, binding Binding) bool {
	ctx, cancel := context.WithTimeout(ctx, livenessTimeout)
	defer cancel()

	return ctx.Err() == nil && binding.Alive(ctx) && ctx.Err() == nil
}
