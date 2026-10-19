package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const readinessTimeout = 2 * time.Second

// Ensure reuses a healthy daemon or launches one only when its endpoint is absent.
// Server is an opaque incarnation token. Composition prepares the complete child
// arguments only when launch is needed; preparation must honor ctx. An empty
// executable selects the current binary. A started child outlives the caller.
func Ensure(ctx context.Context, server, executable string, prepareLaunch func(context.Context) ([]string, error)) (*Client, error) {
	c, err := Connect(server)
	if err != nil {
		return nil, err
	}

	_, err = c.Ping(ctx)
	if err == nil {
		return c, nil
	}

	if !isMissingEndpoint(err) {
		return nil, err
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

	exited, err := startProcess(ctx, executable, arguments)
	if err != nil {
		return nil, err
	}

	if err := c.waitReady(ctx, exited); err != nil {
		return nil, err
	}

	return c, nil
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

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := c.Ping(ctx); err == nil {
			return nil
		}

		select {
		case err := <-exited:
			exited = nil
			if err != nil {
				return fmt.Errorf("status daemon exited before readiness: %w (log: %s)", err, c.LogPath())
			}

		case <-ctx.Done():
			return fmt.Errorf("status daemon startup: %w (log: %s)", ctx.Err(), c.LogPath())
		case <-ticker.C:
		}
	}
}
