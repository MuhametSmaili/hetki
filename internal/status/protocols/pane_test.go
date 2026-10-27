package protocols_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/protocols"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func report(payload string) []byte { return []byte("\x1b]7501;" + payload + "\a") }
func marker(payload string) []byte { return []byte("\x1b]133;" + payload + "\a") }

func TestCharmProgramStatusReports(t *testing.T) {
	cases := []struct {
		state   ansi.ProgramState
		want    status.State
		outcome status.Outcome
	}{
		{ansi.ProgramStateIdle, status.Idle, status.NoOutcome},
		{ansi.ProgramStateWorking, status.Working, status.NoOutcome},
		{ansi.ProgramStateBlocked, status.Blocked, status.NoOutcome},
		{ansi.ProgramStateDone, status.Unknown, status.Completed},
		{ansi.ProgramStateError, status.Unknown, status.Failed},
		{ansi.ProgramStateClear, status.Withdrawn, status.NoOutcome},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			input := ansi.SetProgramStatus(ansi.ProgramStatus{
				State: tc.state, App: "charm", Kind: ansi.ProgramStatusKindPermission,
				Progress: 42, HasProgress: true, Title: "Build 日本", Message: "Waiting: approve?",
			})
			require.NotEmpty(t, input)
			pane := protocols.New(protocols.Options{ProgramStatus: true})
			pane.Feed(report("state=working"))

			var publish bool
			for i := range len(input) {
				_, changed := pane.Feed([]byte{input[i]})
				publish = publish || changed
			}
			observation := pane.Snapshot()

			require.True(t, publish)
			require.Equal(t, tc.want, observation.Activity)
			require.Equal(t, tc.outcome, observation.Outcome)
		})
	}
}

func TestShellMarkersDoNotChangeApplicationStatus(t *testing.T) {
	cases := []struct {
		state   string
		outcome status.Outcome
		want    status.State
	}{
		{"clear", status.NoOutcome, status.Withdrawn},
		{"idle", status.NoOutcome, status.Idle},
		{"working", status.NoOutcome, status.Working},
		{"blocked", status.NoOutcome, status.Blocked},
		{"done", status.Completed, status.Unknown},
		{"error", status.Failed, status.Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			pane := protocols.New(protocols.Options{ProgramStatus: true})
			pane.Feed(report("state=" + tc.state))
			before := pane.Snapshot()

			for _, payload := range []string{"A;cl=line", "B", "C", "D;0", "A"} {
				_, publish := pane.Feed(marker(payload))

				require.False(t, publish)
				require.Equal(t, before, pane.Snapshot())
			}
			require.Equal(t, tc.want, pane.Snapshot().Activity)
			require.Equal(t, tc.outcome, pane.Snapshot().Outcome)
		})
	}
}

func TestShellMarkersAloneDoNotCreateApplicationActivity(t *testing.T) {
	pane := protocols.New(protocols.Options{ProgramStatus: true})

	for _, payload := range []string{"A", "B", "C", "D;0", "A"} {
		_, publish := pane.Feed(marker(payload))

		require.False(t, publish)
		require.Equal(t, status.Unreported, pane.Snapshot().Activity)
	}
}

func TestReportsAreDispatchedBetweenIgnoredShellMarkers(t *testing.T) {
	pane := protocols.New(protocols.Options{ProgramStatus: true})
	input := append(marker("C"), report("state=blocked")...)
	input = append(input, marker("D;0")...)
	input = append(input, marker("A;k=s")...)

	observation, publish := pane.Feed(input)

	require.True(t, publish)
	require.Equal(t, status.Blocked, observation.Activity)
	input = append(marker("A"), report("state=working")...)
	observation, publish = pane.Feed(input)
	require.True(t, publish)
	require.Equal(t, status.Working, observation.Activity)
}

func TestResetAndForegroundChangeHaveDifferentLifetimes(t *testing.T) {
	pane := protocols.New(protocols.Options{ProgramStatus: true})
	pane.Feed(report("state=done"))
	observed := pane.Snapshot().OutcomeSince

	pane.ForegroundChanged()
	afterExit := pane.Snapshot()
	pane.Reset()
	afterGap := pane.Snapshot()

	require.Equal(t, status.Completed, afterExit.Outcome)
	require.False(t, observed.IsZero())
	require.Equal(t, observed, afterExit.OutcomeSince)
	require.Equal(t, protocols.Observation{Activity: status.Unreported}, afterGap)
}

func TestRepeatedRootOutcomeRetainsObservationTime(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{"repeated success", report("state=done")},
		{"child activity", report("state=working:id=child")},
		{"child withdrawal", report("state=clear:id=child")},
		{"invalid state", report("state=invalid")},
		{"invalid root report", report("state=working:id=bad!")},
		{"render zones", append(marker("A"), marker("C")...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pane := protocols.New(protocols.Options{ProgramStatus: true})
				first, _ := pane.Feed(report("state=done"))
				time.Sleep(6 * time.Second)

				pane.Feed(tc.input)
				repeated, publish := pane.Feed(report("state=done"))

				require.True(t, publish)
				require.Equal(t, status.Completed, repeated.Outcome)
				require.False(t, first.OutcomeSince.IsZero())
				require.Equal(t, first.OutcomeSince, repeated.OutcomeSince)
			})
		})
	}
}

func TestRISWithdrawsAndDiscardsPartialReport(t *testing.T) {
	pane := protocols.New(protocols.Options{ProgramStatus: true})
	pane.Feed(report("state=working"))
	pane.Feed([]byte("\x1b]7501;state="))

	observation, publish := pane.Feed([]byte("\x1bcidle\a"))

	require.True(t, publish)
	require.Equal(t, status.Withdrawn, observation.Activity)
	require.Equal(t, observation, pane.Snapshot())
}

func TestSelectionAndChunkBoundaries(t *testing.T) {
	input := append(report("state=working"), marker("C")...)
	for split := 0; split <= len(input); split++ {
		pane := protocols.New(protocols.Options{})

		_, first := pane.Feed(input[:split])
		_, second := pane.Feed(input[split:])
		observation := pane.Snapshot()

		require.False(t, first || second)
		require.Equal(t, status.Unreported, observation.Activity)
	}
}
