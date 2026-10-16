// Package osc7501 observes the state-only projection of Program Status reports.
// It validates unused metadata but retains only record IDs and states. It does
// not implement terminal replies, metadata inheritance or presentation policy.
package osc7501

import "strings"

// MaxRecords is the per-pane least-recently-updated record cap.
const MaxRecords = 64

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

type record struct {
	id    string
	state State
}

// Observer belongs to one pane's applicable protocol scope. Its zero value is
// ready to use. Calls must be serialized and it must not be copied after use.
// Records are ordered oldest update first, avoiding an unbounded sequence counter.
type Observer struct {
	records  []record
	observed bool
}

// Handle consumes the payload after "7501;" from a complete, size-checked OSC.
// Validation finishes before any record changes. Payload is not retained.
func (o *Observer) Handle(payload []byte) Result {
	if len(payload) > maxPayloadBytes {
		return Rejected
	}

	report, result := parse(string(payload))
	if result != Applied {
		return result
	}

	o.observed = true
	if report.clear {
		o.clear(report.id)
		return Applied
	}

	o.store(report.id, report.state)
	return Applied
}

// Observed reports whether any report, including an explicit clear, was accepted
// since Reset. It distinguishes withdrawal from never having received evidence.
func (o *Observer) Observed() bool { return o.observed }

// Lookup returns the current state for an exact ID. An empty ID is the root;
// children do not replace or aggregate into it.
func (o *Observer) Lookup(id string) (State, bool) {
	for _, row := range o.records {
		if row.id == id {
			return row.state, true
		}
	}
	return "", false
}

// Reset removes all records and the observed claim for RIS or lost continuity.
func (o *Observer) Reset() {
	*o = Observer{}
}

// EndActivity handles an observed prompt or attached-process exit. Working,
// blocked and idle are dropped; explicit done/error survive until cleared/reset.
// It returns whether records changed. Silence alone must never invoke this method.
func (o *Observer) EndActivity() bool {
	kept := o.records[:0]
	for _, row := range o.records {
		if row.state == Done || row.state == Error {
			kept = append(kept, row)
		}
	}

	changed := len(kept) != len(o.records)
	clear(o.records[len(kept):])
	o.records = kept
	return changed
}

func (o *Observer) store(id string, state State) {
	for index, row := range o.records {
		if row.id == id {
			copy(o.records[index:], o.records[index+1:])
			o.records[len(o.records)-1] = record{id: row.id, state: state}
			return
		}
	}

	row := record{id: strings.Clone(id), state: state}
	if len(o.records) == MaxRecords {
		copy(o.records, o.records[1:])
		o.records[len(o.records)-1] = row
		return
	}

	if len(o.records) == cap(o.records) {
		capacity := min(MaxRecords, max(4, cap(o.records)*2))
		records := make([]record, len(o.records), capacity)
		copy(records, o.records)
		o.records = records
	}
	o.records = append(o.records, row)
}

func (o *Observer) clear(id string) {
	kept := o.records[:0]
	for _, row := range o.records {
		if id != "" && row.id != id && !strings.HasPrefix(row.id, id+"/") {
			kept = append(kept, row)
		}
	}

	clear(o.records[len(kept):])
	o.records = kept
}
