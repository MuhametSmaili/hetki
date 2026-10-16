package osc133

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundarySequence(t *testing.T) {
	var observer Observer
	cases := []struct {
		payload string
		kind    Kind
		want    Snapshot
	}{
		{"A;cl=line", Prompt, Snapshot{Phase: AtPrompt}},
		{"A", Prompt, Snapshot{Phase: AtPrompt}},
		{"B", Input, Snapshot{Phase: Editing}},
		{"C", Execute, Snapshot{Phase: Running, Invocation: 1}},
		{"D;7", Finish, Snapshot{Phase: Finished, Invocation: 1, Exit: ExitStatus{Code: 7, Known: true}}},
		{"A", Prompt, Snapshot{Phase: AtPrompt}},
		{"C", Execute, Snapshot{Phase: Running, Invocation: 2}},
		{"A", Prompt, Snapshot{Phase: AtPrompt}},
	}
	for _, tc := range cases {
		kind := observer.Handle([]byte(tc.payload))

		require.Equal(t, tc.kind, kind)
		require.Equal(t, tc.want, observer.Snapshot(), tc.payload)
	}
}

func TestReportedExitStatusIsOptionalAndNotInferred(t *testing.T) {
	cases := []struct {
		payload string
		exit    ExitStatus
	}{
		{"D", ExitStatus{}},
		{"D;", ExitStatus{}},
		{"D;0", ExitStatus{Known: true}},
		{"D;0007", ExitStatus{Code: 7, Known: true}},
		{"D;130", ExitStatus{Code: 130, Known: true}},
		{"D;255", ExitStatus{Code: 255, Known: true}},
		{"D;256", ExitStatus{}},
		{"D;-1", ExitStatus{}},
		{"D;+0", ExitStatus{}},
		{"D; 0", ExitStatus{}},
		{"D;no", ExitStatus{}},
		{"D;9999999999999999999999999999", ExitStatus{}},
	}
	for _, tc := range cases {
		t.Run(tc.payload, func(t *testing.T) {
			var observer Observer
			observer.Handle([]byte("C"))

			kind := observer.Handle([]byte(tc.payload))

			require.Equal(t, Finish, kind)
			require.Equal(t, Snapshot{Phase: Finished, Invocation: 1, Exit: tc.exit}, observer.Snapshot())
		})
	}
}

func TestAmbiguousOrUnmatchedBoundariesLoseConfidence(t *testing.T) {
	cases := []struct {
		name    string
		markers []string
	}{
		{"end without start", []string{"D;0"}},
		{"editing aborted", []string{"A", "B", "D;0"}},
		{"duplicate start", []string{"A", "C", "C"}},
		{"duplicate start then end", []string{"A", "C", "C", "D;0"}},
		{"duplicate end", []string{"C", "D;0", "D;0"}},
		{"input during execution", []string{"C", "B", "D;0"}},
		{"new start after ambiguous input", []string{"C", "C", "C"}},
		{"new execution without intervening prompt", []string{"C", "D;0", "C"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var observer Observer

			for _, marker := range tc.markers {
				observer.Handle([]byte(marker))
			}

			require.Equal(t, Snapshot{}, observer.Snapshot())
			observer.Handle([]byte("A"))
			observer.Handle([]byte("C"))
			require.Equal(t, Running, observer.Snapshot().Phase)
		})
	}
}

func TestUnsupportedMarkersDoNotManufactureBoundaries(t *testing.T) {
	for _, payload := range []string{
		"", "AA", " A", "A;", "A;k=s", "A;aid=child", "A;cl=line;k=s",
		"A;cl=unknown", "B;unknown=x", "C;cmdline=echo", "D;0;aid=child",
		"N", "P", "133;A", "A\x00", strings.Repeat("A", maxPayloadBytes+1),
	} {
		t.Run(payload[:min(40, len(payload))], func(t *testing.T) {
			var observer Observer
			observer.Handle([]byte("C"))
			before := observer.Snapshot()

			kind := observer.Handle([]byte(payload))

			require.Equal(t, None, kind)
			require.Equal(t, before, observer.Snapshot())
		})
	}
}

func TestResetCannotMatchAnOldCommandEnd(t *testing.T) {
	var observer Observer
	observer.Handle([]byte("C"))
	before := observer.Snapshot().Invocation

	observer.Reset()
	observer.Handle([]byte("D;0"))

	require.Equal(t, Snapshot{}, observer.Snapshot())
	observer.Handle([]byte("A"))
	observer.Handle([]byte("C"))
	require.Greater(t, observer.Snapshot().Invocation, before)
}

func TestInvocationCounterDoesNotWrap(t *testing.T) {
	observer := Observer{sequence: ^uint64(0)}

	observer.Handle([]byte("A"))
	observer.Handle([]byte("C"))

	require.Equal(t, Snapshot{}, observer.Snapshot())
}

func BenchmarkShellBoundaries(b *testing.B) {
	var observer Observer
	markers := [][]byte{[]byte("A;cl=line"), []byte("B"), []byte("C"), []byte("D;130")}
	b.ReportAllocs()

	for b.Loop() {
		for _, marker := range markers {
			observer.Handle(marker)
		}
	}
}
