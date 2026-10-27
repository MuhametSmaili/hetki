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

const outcomeDisplayDuration = 5 * time.Second

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

	// OutcomeEnded means the reporting foreground attribution has ended. It does
	// not prove OS process exit: suspension also ends foreground attribution.
	OutcomeEnded bool

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
	if v.OutcomeEnded && v.Outcome == NoOutcome {
		return fmt.Errorf("ended outcome requires an outcome")
	}
	return nil
}

// OutcomeAt limits outcomes to native validation and the brief display interval
// for success or ended errors. Errors from a foreground reporter wait for new evidence.
// Repeated reports and inventory renewals do not restart that interval.
func (v Value) OutcomeAt(now time.Time) Outcome {
	if v.State == Unknown && !v.ValidUntil.IsZero() && now.Before(v.ValidUntil) && !v.outcomeExpired(now) {
		switch v.Outcome {
		case Completed, Failed:
			return v.Outcome
		}
	}
	return NoOutcome
}

func (v Value) At(now time.Time) State {
	switch v.State {
	case Unreported, Withdrawn:
		return v.State
	case Unknown:
		return v.outcomeStateAt(now)
	case Idle, Working, Blocked:
		if !v.ValidUntil.IsZero() && now.Before(v.ValidUntil) {
			return v.State
		}
	}
	return Unknown
}

func (v Value) outcomeStateAt(now time.Time) State {
	if v.OutcomeEnded {
		if !now.Before(v.ValidUntil) || v.outcomeExpired(now) {
			return Withdrawn
		}
		return Unknown
	}
	if now.Before(v.ValidUntil) && v.outcomeExpired(now) {
		return Idle
	}
	return Unknown
}

func (v Value) outcomeDeadline() time.Time {
	if v.OutcomeSince.IsZero() {
		return time.Time{}
	}
	switch v.Outcome {
	case Completed:
	case Failed:
		if !v.OutcomeEnded {
			return time.Time{}
		}
	default:
		return time.Time{}
	}
	return v.OutcomeSince.Add(outcomeDisplayDuration)
}

func (v Value) outcomeExpired(now time.Time) bool {
	deadline := v.outcomeDeadline()
	return !deadline.IsZero() && !now.Before(deadline)
}

// NextChange returns the next effective state/outcome deadline, or zero when
// time alone cannot change this value. It starts no timer and performs no I/O.
func (v Value) NextChange(now time.Time) time.Time {
	if !v.ValidUntil.After(now) {
		return time.Time{}
	}
	state := v.At(now)
	if state == Withdrawn || state == Unreported || state == Unknown && v.OutcomeAt(now) == NoOutcome {
		return time.Time{}
	}

	due := v.ValidUntil
	if deadline := v.outcomeDeadline(); deadline.After(now) && deadline.Before(due) {
		due = deadline
	}
	return due
}

// Leased limits activity and outcomes to the inventory lease. A zero evidence
// deadline means that only the lease applies.
func (v Value) Leased(lease time.Time) Value {
	if v.State != Working && v.State != Blocked && v.State != Idle && v.Outcome == NoOutcome {
		return v
	}
	if v.ValidUntil.IsZero() || lease.Before(v.ValidUntil) {
		v.ValidUntil = lease
	}
	return v
}
