package tui

import (
	"time"

	"github.com/MSmaili/hetki/internal/status"
)

const statusSuccessDuration = 5 * time.Second

func displayedStatus(value status.Value, now time.Time) status.Value {
	current := status.Value{State: value.At(now), Outcome: value.OutcomeAt(now)}
	deadline := successDisplayDeadline(value)
	if current.Outcome == status.Completed && !deadline.IsZero() && !now.Before(deadline) {
		return status.Value{State: status.Idle}
	}
	return current
}

func successDisplayDeadline(value status.Value) time.Time {
	if value.Outcome != status.Completed || value.OutcomeSince.IsZero() {
		return time.Time{}
	}
	return value.OutcomeSince.Add(statusSuccessDuration)
}

func nextStatusChange(value status.Value, now time.Time) time.Time {
	if !value.ValidUntil.After(now) || value.State == status.Withdrawn {
		return time.Time{}
	}
	if value.At(now) == status.Unknown && value.OutcomeAt(now) == status.NoOutcome {
		return time.Time{}
	}

	due := value.ValidUntil
	if deadline := successDisplayDeadline(value); deadline.After(now) {
		due = earlierStatusTime(due, deadline)
	}
	return due
}
