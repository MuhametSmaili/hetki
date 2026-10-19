package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Keep the underlying buffer private so all copies pass through Write.
type boundedBuffer struct {
	buffer    bytes.Buffer
	max       int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.buffer.Len() {
		b.truncated = true
		return 0, fmt.Errorf("command output exceeds %d bytes", b.max)
	}
	return b.buffer.Write(p)
}

func (b *boundedBuffer) ReadFrom(r io.Reader) (int64, error) {
	// A writer-only wrapper enforces Write's limit without recursing here.
	n, err := io.Copy(struct{ io.Writer }{b}, r)
	if err != nil {
		// WaitDelay can close the pipe even when an exit error masks its error.
		b.truncated = true
	}
	return n, err
}

func (b *boundedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

func commandOutput(ctx context.Context, timeout time.Duration, maxBytes int, name string, args ...string) ([]byte, error) {
	return commandOutputEnv(ctx, timeout, maxBytes, nil, name, args...)
}

// On failure, stdout is available for structured error fields only; callers
// must check the error before accepting any output as a successful result.
func commandOutputEnv(ctx context.Context, timeout time.Duration, maxBytes int, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	defer killCommandGroup(cmd)
	if env != nil {
		cmd.Env = env
	}
	configureCommandLifecycle(cmd)
	output := &boundedBuffer{max: maxBytes}
	stderr := &boundedBuffer{max: maxProcessOutputBytes}
	cmd.Stdout, cmd.Stderr = output, stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return output.Bytes(), commandFailure(err, stderr)
	}
	return output.Bytes(), nil
}

func configureCommandLifecycle(cmd *exec.Cmd) {
	cmd.WaitDelay = 2 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

func killCommandGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
