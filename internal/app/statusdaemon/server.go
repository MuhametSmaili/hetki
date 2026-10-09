package statusdaemon

import (
	"context"
	"net"
	"sync"
	"time"
)

const maxClients = 16

type daemon struct {
	binding Binding
	source  Source
	cancel  context.CancelFunc
	info    Info
	logs    *daemonLog
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
			if !d.checkLifetime(ctx) {
				return
			}
		}
	}
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
