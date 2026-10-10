package statusobserver

import (
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/adapters/osc7501"
)

func osc7501Value(observer *osc7501.Observer) status.Value {
	state, found := observer.Lookup("")
	if !found {
		return status.Value{State: status.Withdrawn}
	}

	switch state {
	case osc7501.Idle:
		return status.Value{State: status.Idle}
	case osc7501.Working:
		return status.Value{State: status.Working}
	case osc7501.Blocked:
		return status.Value{State: status.Blocked}
	default:
		// Done/error are retained protocol outcomes, not inferred idle activity.
		return status.Value{State: status.Unknown}
	}
}
