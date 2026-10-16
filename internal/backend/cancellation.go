package backend

import (
	"context"
	"os/exec"
)

// CanceledProcessError retains a process exit from SIGINT/SIGTERM or the owner's
// successful kill under context.Canceled. Only the process owner may create it.
// Callers must check their own context before suppressing an interruption;
// foreground-group signals can reach a child before its parent's context.
// Diagnostics and other failures must be wrapped or joined separately.
type CanceledProcessError struct {
	Cause *exec.ExitError
}

func (e *CanceledProcessError) Error() string { return e.Cause.Error() }
func (e *CanceledProcessError) Unwrap() error { return e.Cause }

// IsCancellationOnly reports whether every cause is cancellation. Deadlines and
// failures joined to cancellation are not quiet cancellation outcomes. The caller
// must also establish that its context was canceled before suppressing output.
func IsCancellationOnly(err error) bool {
	switch cause := err.(type) {
	case *CanceledProcessError:
		return true
	case interface{ Unwrap() []error }:
		children := cause.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !IsCancellationOnly(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return IsCancellationOnly(cause.Unwrap())
	default:
		return err == context.Canceled
	}
}
