// Package osc7501 observes the state-only projection of Program Status reports.
// It validates unused metadata and child records but retains only the root
// state. It does not implement terminal replies, metadata inheritance or
// presentation policy.
package osc7501

type State string

const (
	Idle    State = "idle"
	Working State = "working"
	Blocked State = "blocked"
	Done    State = "done"
	Error   State = "error"
)

// Result distinguishes accepted reports from queries, invalid reports and
// syntactically valid input that has no recognized state. No result sends bytes.
type Result uint8

const (
	Ignored Result = iota
	Query
	Rejected
	Applied
)

// Observer belongs to one pane's applicable protocol scope. Its zero value is
// ready to use. Calls must be serialized. Child records (reports with an id)
// are validated but not retained: they never replace or aggregate into the
// root, which is the only record a pane displays.
type Observer struct {
	root     State
	observed bool
}

// Handle consumes the payload after "7501;" from a complete, size-checked OSC.
// Validation finishes before the root changes. Payload is not retained.
func (o *Observer) Handle(payload []byte) Result {
	if len(payload) > maxPayloadBytes {
		return Rejected
	}

	report, result := parse(string(payload))
	if result != Applied {
		return result
	}

	o.observed = true
	if report.id == "" {
		// A clear report has no state, so it withdraws the root.
		o.root = report.state
	}
	return Applied
}

// Observed reports whether any report, including an explicit clear, was accepted
// since Reset. It distinguishes withdrawal from never having received evidence.
func (o *Observer) Observed() bool { return o.observed }

// Root returns the root state, or false when none was reported or it was withdrawn.
func (o *Observer) Root() (State, bool) { return o.root, o.root != "" }

// Reset removes the root and the observed claim for RIS or lost continuity.
func (o *Observer) Reset() {
	*o = Observer{}
}

// EndActivity handles an observed prompt or attached-process exit. Working,
// blocked and idle are dropped; explicit done/error survive until cleared/reset.
// It returns whether the root changed. Silence alone must never invoke this method.
func (o *Observer) EndActivity() bool {
	switch o.root {
	case Idle, Working, Blocked:
		o.root = ""
		return true
	default:
		return false
	}
}
