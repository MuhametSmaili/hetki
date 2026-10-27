package protocols

import (
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
)

// Observation is a protocol-neutral value copy. Only root application state is
// projected; child records remain private. Outcomes do not imply idle activity.
type Observation struct {
	Activity status.State
	Outcome  status.Outcome
	// Start of the current root outcome, preserved across repeated reports.
	// Captured before chunk coalescing so intervening resets are not lost.
	OutcomeSince time.Time
	// OutcomeEnded records the loss of foreground attribution, not OS process exit.
	OutcomeEnded bool
}
