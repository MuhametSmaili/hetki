// Package status defines source-neutral effective pane status.
package status

import "time"

type State string

const (
	Unreported State = ""
	Withdrawn  State = "withdrawn"
	Unknown    State = "unknown"
	Idle       State = "idle"
	Working    State = "working"
	Blocked    State = "blocked"
)

// PaneRef identifies server and pane lifetimes, not reusable names or PIDs.
type PaneRef struct {
	Server     string
	ID         string
	Generation string
}

type Value struct {
	State State
	// Cache validation deadline, not a producer heartbeat.
	ValidUntil time.Time
}

func (v Value) At(now time.Time) State {
	switch v.State {
	case Unreported, Withdrawn, Unknown:
		return v.State
	case Idle, Working, Blocked:
		if !v.ValidUntil.IsZero() && now.Before(v.ValidUntil) {
			return v.State
		}
	}
	return Unknown
}
