package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// runCommand owns a noninteractive process group and bounded stdout/stderr.
// It waits for all os/exec copy workers before returning; no child owns the TTY.
func runCommand(ctx context.Context, spec Command, request Request) (commandResult, error) {
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
	return runCapturedCommand(ctx, args, input, duration)
}

func runCapturedCommand(ctx context.Context, args []string, input []byte, timeout time.Duration) (commandResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout := boundedOutput{limit: maxOutput, cancel: cancel}
	stderr := boundedOutput{limit: 8 * 1024, cancel: cancel}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := runProcessGroup(cmd)
	result := commandResult{exitedSuccessfully: cmd.ProcessState != nil && cmd.ProcessState.Success()}
	if stdout.exceeded || stderr.exceeded {
		err = errors.Join(err, ErrOutputLimit)
	}
	if err != nil {
		cause := errors.Join(err, ctx.Err())
		if result.exitedSuccessfully {
			cause = fmt.Errorf("command exited successfully, but output capture or cleanup failed: %w", cause)
		}
		diagnostic := strings.TrimSpace(stderr.buffer.String())
		return result, fmt.Errorf("action command %q: %w: %s", args[0], cause, diagnostic)
	}
	result.output = stdout.buffer.Bytes()
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
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (w *boundedOutput) Write(data []byte) (int, error) {
	size := len(data)
	if remaining := w.limit - w.buffer.Len(); size > remaining {
		data = data[:remaining]
		w.exceeded = true
		w.cancel()
	}
	_, _ = w.buffer.Write(data)
	return size, nil
}
