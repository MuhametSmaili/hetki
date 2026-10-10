// Package osc133 observes ordinary FinalTerm A/B/C/D shell boundaries. It stores
// no command text or history and does not infer application activity or outcomes.
package osc133

// Kind identifies an accepted marker. None means unsupported or malformed input.
type Kind uint8

const (
	None Kind = iota
	Prompt
	Input
	Execute
	Finish
)

// Phase describes confidence in the current shell boundary, not application work.
type Phase uint8

const (
	Unknown Phase = iota
	AtPrompt
	Editing
	Running
	Finished
)

// ExitStatus distinguishes an absent/invalid report from an explicit zero. Code
// is the shell's numeric status; 130 does not by itself prove a cancellation.
type ExitStatus struct {
	Code  uint8
	Known bool
}

// Snapshot is a value copy. Invocation identifies an observed C within this
// observer's lifetime; zero means no confidently matched invocation. A Finished
// phase proves a matched boundary, even when the exit status is unavailable.
type Snapshot struct {
	Phase      Phase
	Invocation uint64
	Exit       ExitStatus
}

// Observer tracks one current invocation in a caller-validated pane/root stream.
// Its zero value is ready. Calls must be serialized. No foreground PID is inferred
// from markers, and nested shells/job control cannot be attributed by A/B/C/D alone.
type Observer struct {
	current   Snapshot
	sequence  uint64
	ambiguous bool
}

// Handle consumes the payload after "133;" in one complete OSC. Unsupported
// markers leave state untouched. Only Prompt is a prompt-start lifecycle event.
func (o *Observer) Handle(payload []byte) Kind {
	kind, exit := parse(payload)
	switch kind {
	case Prompt:
		o.current = Snapshot{Phase: AtPrompt}
		o.ambiguous = false
	case Input:
		if o.current.Phase == AtPrompt || o.current.Phase == Editing {
			o.current.Phase = Editing
		} else {
			o.loseConfidence()
		}
	case Execute:
		o.start()
	case Finish:
		if o.current.Phase == Running {
			o.current.Phase = Finished
			o.current.Exit = exit
		} else {
			o.loseConfidence()
		}
	}
	return kind
}

// Snapshot returns current bounded lifecycle state without exposing mutable data.
func (o *Observer) Snapshot() Snapshot { return o.current }

// Reset loses confidence after RIS, a gap, root replacement or process exit.
// The local counter is retained so a later command cannot reuse an invocation ID.
func (o *Observer) Reset() {
	o.current = Snapshot{}
	o.ambiguous = false
}

func (o *Observer) start() {
	if o.ambiguous || o.current.Phase == Running || o.current.Phase == Finished || o.sequence == ^uint64(0) {
		o.loseConfidence()
		return
	}

	o.sequence++
	o.current = Snapshot{Phase: Running, Invocation: o.sequence}
}

func (o *Observer) loseConfidence() {
	o.current = Snapshot{}
	o.ambiguous = true
}
