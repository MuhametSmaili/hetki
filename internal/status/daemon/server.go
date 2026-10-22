package daemon

import (
	"context"
	"net"
	"sync"
	"time"
)

const (
	maxClients = 16

	// Subscribers use at most half of the slots, so ping and stop always get one.
	maxSubscribers = maxClients / 2
)

type daemon struct {
	binding     Binding
	source      Source
	cancel      context.CancelFunc
	info        Info
	logs        *daemonLog
	subscribers chan struct{}

	// Nil when the startup binary could not be recorded; the daemon then cannot
	// detect that an update replaced it and keeps running until stopped.
	executable *executableImage

	lifecycle sync.Mutex
}

func (d *daemon) run(ctx context.Context, listener *net.UnixListener) error {
	var workers sync.WaitGroup
	defer func() {
		d.cancel()
		_ = listener.Close()
		workers.Wait()
	}()

	workers.Go(func() {
		<-ctx.Done()
		_ = listener.Close()
	})
	workers.Go(func() {
		d.watchLifetime(ctx)
	})

	return d.accept(ctx, listener, &workers)
}

func (d *daemon) watchLifetime(ctx context.Context) {
	defer d.logs.recordPanic()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			if !d.checkLifetime(ctx) || d.executableReplaced() {
				return
			}
		}
	}
}

func (d *daemon) executableReplaced() bool {
	if d.executable == nil {
		return false
	}

	replaced, err := d.executable.replaced()
	if err != nil {
		d.logs.repeatedFailure("executable_check_failed", err)
		return false
	}

	if replaced {
		d.logs.info("executable_replaced")
		d.cancel()
	}
	return replaced
}

func (d *daemon) accept(ctx context.Context, listener *net.UnixListener, workers *sync.WaitGroup) error {
	slots := make(chan struct{}, maxClients)
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}

		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}

		workers.Go(func() {
			defer func() {
				<-slots
			}()

			d.handle(ctx, conn)
		})
	}
}
