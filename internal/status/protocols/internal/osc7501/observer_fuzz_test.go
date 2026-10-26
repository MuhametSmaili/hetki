package osc7501

import "testing"

func FuzzObserverAtomicity(f *testing.F) {
	f.Add([]byte("state=idle:msg=/w=="))
	f.Add([]byte("state=clear:id=build"))
	f.Add([]byte("state=done:id=child"))
	f.Add([]byte("state=idle:id=bad//path"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var observer Observer
		observer.Handle([]byte("state=working"))
		before := observer

		result := observer.Handle(payload)

		if result != Applied && observer != before {
			t.Fatal("unaccepted report changed the observer")
		}
		if state, found := observer.Root(); found && recognizedState(string(state)) == "" {
			t.Fatal("unrecognized root state retained")
		}
	})
}
