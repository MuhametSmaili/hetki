package protocols

import (
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/protocols/internal/osc133"
)

// Outcome is an explicit application result, separate from current activity.
type Outcome uint8

const (
	NoOutcome Outcome = iota
	Completed
	Failed
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
	Outcome  Outcome
	Shell    Shell
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
