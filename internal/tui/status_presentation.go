package tui

import (
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
)

func displayedStatus(value status.Value, now time.Time) status.Value {
	return status.Value{State: value.At(now), Outcome: value.OutcomeAt(now)}
}

func nextStatusChange(value status.Value, now time.Time) time.Time {
	return value.NextChange(now)
}
