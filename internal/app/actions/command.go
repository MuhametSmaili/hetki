package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

var ErrOutputLimit = errors.New("action output limit exceeded")

// A successful exit is independent of capture and cleanup errors. Only providers
// require complete output; final mutations must not be retried after exit zero.
type commandResult struct {
	output             []byte
	exitedSuccessfully bool
}

func commandTimeout(value string) (time.Duration, error) {
	if value == "" {
		return 30 * time.Second, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration > 10*time.Minute {
		return 0, fmt.Errorf("timeout must be a duration greater than zero and at most 10m")
	}
	return duration, nil
}

func runCommand(ctx context.Context, spec Command, request Request) (commandResult, error) {
	var output bytes.Buffer
	result, err := runCommandTo(ctx, spec, request, &output)
	if err == nil {
		result.output = output.Bytes()
	}
	return result, err
}

// runCommandTo owns a noninteractive process group and bounded stdout/stderr.
// Output is consumed serially by an os/exec copy worker, joined before return.
// The writer must not block; a write error cancels the process group.
func runCommandTo(ctx context.Context, spec Command, request Request, output io.Writer) (commandResult, error) {
	args, err := resolveArgs(spec.Argv, request)
	if err != nil {
		return commandResult{}, err
	}

	duration, err := commandTimeout(spec.Timeout)
	if err != nil {
		return commandResult{}, err
	}

	input, err := json.Marshal(request)
	if err != nil {
		return commandResult{}, fmt.Errorf("encode action context: %w", err)
	}
	if len(input) > maxOutput {
		return commandResult{}, fmt.Errorf("action context exceeds 4 MiB")
	}
	return runCapturedCommand(ctx, args, input, duration, output)
}

func runCapturedCommand(ctx context.Context, args []string, input []byte, timeout time.Duration, output io.Writer) (commandResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var diagnostic bytes.Buffer
	stdout := boundedOutput{writer: output, limit: maxOutput, cancel: cancel}
	stderr := boundedOutput{writer: &diagnostic, limit: 8 * 1024, cancel: cancel}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := runProcessGroup(cmd)
	result := commandResult{exitedSuccessfully: cmd.ProcessState != nil && cmd.ProcessState.Success()}
	err = errors.Join(err, stdout.err, stderr.err)
	if err != nil {
		cause := errors.Join(err, ctx.Err())
		if result.exitedSuccessfully {
			cause = fmt.Errorf("command exited successfully, but output capture or cleanup failed: %w", cause)
		}
		return result, fmt.Errorf("action command %q: %w: %s", args[0], cause, strings.TrimSpace(diagnostic.String()))
	}
	return result, nil
}

func runProcessGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 200 * time.Millisecond
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}

	err := cmd.Run()
	if cmd.Process != nil {
		// Retire descendants even when the direct child exited before cancellation.
		if killErr := cmd.Cancel(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			err = errors.Join(err, killErr)
		}
	}
	return err
}

type boundedOutput struct {
	writer  io.Writer
	limit   int
	written int
	cancel  context.CancelFunc
	err     error
}

func (w *boundedOutput) Write(data []byte) (int, error) {
	if remaining := w.limit - w.written; len(data) > remaining {
		data = data[:remaining]
		w.err = ErrOutputLimit
	}
	n, err := w.writer.Write(data)
	w.written += n
	w.err = errors.Join(w.err, err)
	if w.err != nil {
		w.cancel()
	}
	return n, w.err
}
