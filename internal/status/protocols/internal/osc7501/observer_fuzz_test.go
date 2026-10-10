package osc7501

import (
	"slices"
	"testing"
)

func FuzzObserverAtomicity(f *testing.F) {
	f.Add([]byte("state=idle:msg=/w=="))
	f.Add([]byte("state=clear:id=build"))
	f.Add([]byte("state=done:id=child"))
	f.Add([]byte("state=idle:id=bad//path"))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var observer Observer
		observer.Handle([]byte("state=working"))
		observer.Handle([]byte("state=blocked:id=child"))
		before := slices.Clone(observer.records)

		result := observer.Handle(payload)

		if result != Applied && !slices.Equal(before, observer.records) {
			t.Fatal("unaccepted report changed stored records")
		}
		if len(observer.records) > MaxRecords || cap(observer.records) > MaxRecords {
			t.Fatal("record storage exceeds cap")
		}
		checkRecords(t, observer.records)
	})
}

func checkRecords(t *testing.T, records []record) {
	t.Helper()
	for _, row := range records {
		if row.id != "" && (!validID(row.id) || !withinIDLimits(row.id)) {
			t.Fatal("invalid record id retained")
		}
		if recognizedState(string(row.state)) == "" {
			t.Fatal("unrecognized state retained")
		}
	}
}
