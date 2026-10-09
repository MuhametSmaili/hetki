package statusdaemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Ensure launches the same binary's internal composition entry point if absent.
// Server is an opaque incarnation token interpreted only by that composition.
func Ensure(ctx context.Context, server, executable string) (*Client, error) {
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

	if err := startProcess(ctx, server, executable); err != nil {
		return nil, err
	}

	if err := c.waitReady(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

func startProcess(ctx context.Context, server, executable string) error {
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return err
		}
	}

	cmd := exec.Command(executable, "__status-daemon", server)
	cmd.Dir = filepath.Dir(executable)

	for _, key := range []string{"PATH", "HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR", "LANG", "TMPDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		_ = cmd.Wait()
	}()

	return nil
}

func (c *Client) waitReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := c.Ping(ctx); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("status daemon startup: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
