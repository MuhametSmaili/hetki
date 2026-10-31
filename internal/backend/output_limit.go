package backend

import "errors"

// ErrOutputLimitExceeded means a command stopped because stdout or stderr
// exceeded its read-time byte limit. Partial output must not be used as state.
var ErrOutputLimitExceeded = errors.New("command output limit exceeded")
