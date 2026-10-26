package daemon

import (
	"context"
	"errors"
	"fmt"
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

var (
	errLaunchLost = errors.New("another daemon won startup")

	// errBuild reports that a daemon from another build started concurrently.
	errBuild = errors.New("status daemon runs another hetki build")
)

// Ensure reuses a healthy daemon of the same build, or waits boundedly for
// ownership before launch. An instance of another build or protocol is stopped
// first, within its own shutdown timeout. Server is an opaque incarnation
// token and build identifies the caller's executable. Composition prepares the
// complete child arguments only when launch is needed; preparation must honor
// ctx. An empty executable selects the current binary. A started child
// outlives the caller.
func Ensure(ctx context.Context, server, build, executable string, prepareLaunch func(context.Context) ([]string, error)) (*Client, error) {
	if !validIdentity(build) {
		return nil, fmt.Errorf("status daemon requires a build identity")
	}

	c, err := Connect(server)
	if err != nil {
		return nil, err
	}

	ready, err := c.reuseOrRetire(ctx, build)
	if err != nil {
		return nil, err
	}
	if ready {
		return c, nil
	}

	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

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

	if err := c.launchReady(ctx, build, executable, arguments); err != nil {
		return nil, err
	}
	return c, nil
}

// reuseOrRetire reports whether an instance of build is ready to reuse. It
// stops instances of other builds until the service settles as Absent. A
// concurrent replacement makes a stop fail with ErrIdentity; the next probe
// then decides again.
func (c *Client) reuseOrRetire(ctx context.Context, build string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()

	for {
		observed, err := c.settle(ctx)
		if err != nil {
			return false, c.startupError(ctx, err, observed.cause)
		}

		switch {
		case observed.state == Absent:
			return false, nil
		case observed.state == Running && observed.info.Build == build:
			return true, nil
		}

		err = c.retire(ctx, observed)
		if err != nil && !errors.Is(err, ErrIdentity) {
			return false, c.startupError(ctx, fmt.Errorf("replace daemon from another hetki build: %w", err), nil)
		}

		select {
		case <-ctx.Done():
			return false, c.startupError(ctx, ctx.Err(), err)
		case <-ticker.C:
		}
	}
}

func (c *Client) launchReady(ctx context.Context, build, executable string, arguments []string) error {
	ticker := time.NewTicker(lifecyclePollInterval)
	defer ticker.Stop()

	for {
		exited, err := startProcess(ctx, executable, arguments)
		if err != nil {
			return err
		}
		err = c.waitReady(ctx, exited)
		if err == nil {
			return c.requireBuild(ctx, build)
		}
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
		ready, err := c.waitForLaunch(ctx, build)
		if err != nil || ready {
			return err
		}
	}
}

// waitForLaunch reports whether a daemon of build is ready to reuse. False
// without an error means that the service settled as Absent, so a launch may
// proceed. Another build that won a concurrent launch is errBuild.
func (c *Client) waitForLaunch(ctx context.Context, build string) (bool, error) {
	observed, err := c.settle(ctx)
	if err != nil {
		return false, c.startupError(ctx, err, observed.cause)
	}

	switch {
	case observed.state == Absent:
		return false, nil
	case observed.state == Running && observed.info.Build == build:
		return true, nil
	default:
		return false, c.startupError(ctx, errBuild, nil)
	}
}

// requireBuild confirms that the instance that became ready runs build.
func (c *Client) requireBuild(ctx context.Context, build string) error {
	ready, err := c.waitForLaunch(ctx, build)
	if err != nil {
		return err
	}
	if !ready {
		return c.startupError(ctx, errors.New("status daemon stopped after readiness"), nil)
	}
	return nil
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
		if err == nil || errors.Is(err, ErrBusy) {
			return nil
		}
		// The child may not own its lock yet, so a missing endpoint is not
		// absence here; only the child's exit ends this wait.
		if !isMissingEndpoint(err) && !interrupted(err) {
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
