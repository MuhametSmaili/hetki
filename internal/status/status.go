// Package status defines source-neutral effective pane status.
package status

import (
	"fmt"
	"time"
)

type State string

const (
	Unreported State = ""
	Withdrawn  State = "withdrawn"
	Unknown    State = "unknown"
	Idle       State = "idle"
	Working    State = "working"
	Blocked    State = "blocked"
)

// Outcome is an explicitly reported result, not evidence that an application is idle.
type Outcome string

const (
	NoOutcome Outcome = ""
	Completed Outcome = "success"
	Failed    Outcome = "error"
)

// PaneRef identifies server and pane lifetimes, not reusable names or PIDs.
type PaneRef struct {
	Server     string
	ID         string
	Generation string
}

type Value struct {
	State   State
	Outcome Outcome

	// First observation of this outcome; repeated evidence retains it.
	// Zero means the observation time is unavailable.
	OutcomeSince time.Time

	// Cache validation deadline, not a producer heartbeat.
	ValidUntil time.Time
}

// Validate checks a reported value. Unreported panes are omitted from snapshots.
func (v Value) Validate() error {
	switch v.State {
	case Withdrawn, Unknown, Idle, Working, Blocked:
	default:
		return fmt.Errorf("invalid pane status %q", v.State)
	}
	if v.Outcome != NoOutcome && (v.State != Unknown || v.Outcome != Completed && v.Outcome != Failed) {
		return fmt.Errorf("invalid outcome %q for pane status %q", v.Outcome, v.State)
	}
	if v.Outcome == NoOutcome && !v.OutcomeSince.IsZero() {
		return fmt.Errorf("outcome observation time requires an outcome")
	}
	return nil
}

// OutcomeAt validates attribution using the same deadline as activity. This is
// not a completion timer: continuous native validation can renew it indefinitely.
func (v Value) OutcomeAt(now time.Time) Outcome {
	if v.State == Unknown && !v.ValidUntil.IsZero() && now.Before(v.ValidUntil) {
		switch v.Outcome {
		case Completed, Failed:
			return v.Outcome
		}
	}
	return NoOutcome
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
