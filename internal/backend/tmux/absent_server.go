package tmux

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
)

type absentServerError struct {
	socket string
	cause  error
}

func (e *absentServerError) Error() string { return e.cause.Error() }
func (e *absentServerError) Unwrap() error { return e.cause }

// tmux uses the same exit status for a missing server and other errors.
func classifyAbsentServer(err error, output, stderr string) error {
	if errors.Is(err, backend.ErrOutputLimitExceeded) {
		return err
	}
	exit, ok := errors.AsType[*exec.ExitError](err)
	if !ok || exit.ExitCode() != 1 || output != "" {
		return err
	}
	message := strings.TrimSuffix(stderr, "\n")
	socket, found := strings.CutPrefix(message, "no server running on ")
	if !found {
		message, found = strings.CutPrefix(message, "error connecting to ")
		if !found {
			return err
		}
		socket, found = strings.CutSuffix(message, " (No such file or directory)")
	}
	if !found || !filepath.IsAbs(socket) || strings.ContainsAny(socket, "\n\r\x00") {
		return err
	}
	return &absentServerError{socket: socket, cause: err}
}
