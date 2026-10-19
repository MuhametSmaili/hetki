package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	readinessTimeout = 2 * time.Second
	startupTimeout   = 5 * time.Second
)

var errLaunchLost = errors.New("another daemon won startup")

// Ensure reuses a healthy daemon or waits boundedly for ownership before launch.
// Server is an opaque incarnation token. Composition prepares the complete child
// arguments only when launch is needed; preparation must honor ctx. An empty
// executable selects the current binary. A started child outlives the caller.
func Ensure(ctx context.Context, server, executable string, prepareLaunch func(context.Context) ([]string, error)) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	c, err := Connect(server)
	if err != nil {
		return nil, err
	}

	ready, err := c.waitForLaunch(ctx)
	if err != nil {
		return nil, err
	}
	if ready {
		return c, nil
	}

	if prepareLaunch == nil {
		return nil, fmt.Errorf("status daemon requires launch preparation")
	}

	arguments, err := prepareLaunch(ctx)
	if err != nil {
		return nil, err
	}

	if err := prepareLog(c.LogPath()); err != nil {
		return nil, fmt.Errorf("prepare daemon log %s: %w", c.LogPath(), err)
	}

	if err := c.launchReady(ctx, executable, arguments); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) launchReady(ctx context.Context, executable string, arguments []string) error {
	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()

	for {
		exited, err := startProcess(ctx, executable, arguments)
		if err != nil {
			return err
		}
		err = c.waitReady(ctx, exited)
		if !errors.Is(err, errLaunchLost) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		// An ownership check is not a reservation. Only retry after the previous
		// child exited, under the original deadline, without redoing preparation.
		ready, err := c.waitForLaunch(ctx)
		if err != nil || ready {
			return err
		}
	}
}

func (c *Client) waitForLaunch(ctx context.Context) (bool, error) {
	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()

	var lastProbe error
	for {
		_, err := c.Ping(ctx)
		if err == nil {
			return true, nil
		}
		if !startupTransition(err) {
			return false, c.startupError(ctx, err, lastProbe)
		}
		lastProbe = err
		if isMissingEndpoint(err) {
			owned, lockErr := endpointOwned(c.path)
			if lockErr != nil || !owned {
				return false, lockErr
			}
		}

		select {
		case <-ctx.Done():
			return false, c.startupError(ctx, ctx.Err(), lastProbe)
		case <-ticker.C:
		}
	}
}

func startupTransition(err error) bool {
	return isMissingEndpoint(err) || errors.Is(err, errEndpointPermissions) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}

func (c *Client) startupError(ctx context.Context, err, lastProbe error) error {
	if ctx.Err() != nil {
		err = errors.Join(ctx.Err(), lastProbe)
	}
	return fmt.Errorf("status daemon startup: %w (log: %s)", err, c.LogPath())
}

func prepareLog(path string) error {
	writer := &logFile{path: path}
	if err := writer.open(); err != nil {
		return err
	}

	return writer.file.Close()
}

func startProcess(ctx context.Context, executable string, arguments []string) (<-chan error, error) {
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}

	cmd := exec.Command(executable, arguments...)
	cmd.Dir = filepath.Dir(executable)

	for _, key := range []string{"PATH", "HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR", "LANG", "TMPDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		close(exited)
	}()

	return exited, nil
}

func (c *Client) waitReady(ctx context.Context, exited <-chan error) error {
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()

	var lastProbe error
	for {
		_, err := c.Ping(ctx)
		if err == nil {
			return nil
		}
		if !startupTransition(err) {
			return c.startupError(ctx, err, lastProbe)
		}
		lastProbe = err

		select {
		case err := <-exited:
			if err != nil {
				return fmt.Errorf("status daemon exited before readiness: %w (log: %s)", err, c.LogPath())
			}
			return errLaunchLost

		case <-ctx.Done():
			return c.startupError(ctx, ctx.Err(), lastProbe)
		case <-ticker.C:
		}
	}
}
