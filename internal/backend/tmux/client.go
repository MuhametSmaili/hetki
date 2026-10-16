package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
)

type Client interface {
	Run(context.Context, ...string) (string, error)
	RunLimited(context.Context, int, ...string) (string, error)
	Execute(context.Context, Action) error
}

type client struct {
	bin string
}

func New() (Client, error) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, fmt.Errorf("tmux not found in PATH")
	}
	return &client{bin: bin}, nil
}

func (c *client) Run(ctx context.Context, args ...string) (string, error) {
	return c.run(ctx, -1, args...)
}

// RunLimited bounds stdout and stderr while reading and stops on overflow.
func (c *client) RunLimited(ctx context.Context, limit int, args ...string) (string, error) {
	if limit < 0 {
		return "", fmt.Errorf("invalid stdout limit %d", limit)
	}
	return c.run(ctx, limit, args...)
}

func (c *client) run(ctx context.Context, limit int, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, c.bin, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	var stdoutLimit, stderrLimit *limitedWriter
	if limit >= 0 {
		stop := func() { _ = cmd.Process.Kill() }
		stdoutLimit = &limitedWriter{buffer: &out, limit: limit, stop: stop}
		stderrLimit = &limitedWriter{buffer: &stderr, limit: 4096, stop: stop}
		cmd.Stdout, cmd.Stderr = stdoutLimit, stderrLimit
		// A descendant retaining the pipes must not defeat cancellation.
		cmd.WaitDelay = 100 * time.Millisecond
	}

	err := runCommand(ctx, cmd)
	if stdoutLimit != nil && stdoutLimit.exceeded {
		err = errors.Join(err, fmt.Errorf("stdout exceeds %d bytes", limit))
	}
	if stderrLimit != nil && stderrLimit.exceeded {
		err = errors.Join(err, fmt.Errorf("stderr exceeds %d bytes", stderrLimit.limit))
	}
	// Raw output: #{q:...} parsers need exact record boundaries.
	output := out.String()

	if err != nil {
		return output, commandError(fmt.Sprintf("tmux %v", args), err, ctx.Err(), stderr.String())
	}

	return output, nil
}

type limitedWriter struct {
	buffer   *bytes.Buffer
	limit    int
	stop     func()
	exceeded bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := w.limit - w.buffer.Len(); n > remaining {
		p = p[:remaining]
		w.exceeded = true
		w.stop()
	}
	_, _ = w.buffer.Write(p)
	return n, nil
}

func (c *client) Execute(ctx context.Context, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.bin, action.Args()...)
	switch action.(type) {
	case SwitchClient, AttachSession:
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := runCommand(ctx, cmd); err != nil {
		return commandError(fmt.Sprintf("tmux %v", action.Args()), err, ctx.Err(), stderr.String())
	}
	return nil
}

func runCommand(ctx context.Context, cmd *exec.Cmd) error {
	killed := false
	cancel := cmd.Cancel
	cmd.Cancel = func() error {
		err := cancel()
		killed = err == nil
		return err
	}

	err := cmd.Run()
	// Run/Wait joins os/exec's context watcher before we read its kill result.
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return err
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return err
	}
	switch status.Signal() {
	case syscall.SIGINT, syscall.SIGTERM:
		// A foreground-group interrupt can reach the child before our kill.
	case syscall.SIGKILL:
		if !killed || ctx.Err() != context.Canceled {
			return err
		}
	default:
		return err
	}

	return &backend.CanceledProcessError{Cause: exitErr}
}

func commandError(operation string, execErr, ctxErr error, stderr string) error {
	if ctxErr != nil {
		execErr = errors.Join(ctxErr, execErr)
	}
	if detail := strings.TrimSpace(stderr); detail != "" {
		return fmt.Errorf("%s failed: %w (%w)", operation, execErr, errors.New(detail))
	}
	return fmt.Errorf("%s failed: %w", operation, execErr)
}
