package protocols

import (
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/protocols/internal/osc133"
)

// ShellPhase describes observed boundaries, not application activity.
type ShellPhase uint8

const (
	ShellUnknown ShellPhase = iota
	AtPrompt
	Editing
	Running
	Finished
)

// Shell describes one bounded command observation. Invocation is local to the
// pane object. ExitKnown distinguishes an unavailable status from an explicit zero.
type Shell struct {
	Phase      ShellPhase
	Invocation uint64
	ExitCode   uint8
	ExitKnown  bool
}

// Observation is a protocol-neutral value copy. Only root application state is
// projected; child records remain private. Outcomes do not imply idle activity.
type Observation struct {
	Activity status.State
	Outcome  status.Outcome
	// Start of the current root outcome, preserved across repeated reports.
	// Captured before chunk coalescing so intervening resets are not lost.
	OutcomeSince time.Time
	Shell        Shell
}

func shellObservation(snapshot osc133.Snapshot) Shell {
	phase := ShellUnknown
	switch snapshot.Phase {
	case osc133.AtPrompt:
		phase = AtPrompt
	case osc133.Editing:
		phase = Editing
	case osc133.Running:
		phase = Running
	case osc133.Finished:
		phase = Finished
	}
	return Shell{
		Phase:      phase,
		Invocation: snapshot.Invocation,
		ExitCode:   snapshot.Exit.Code,
		ExitKnown:  snapshot.Exit.Known,
	}
}
