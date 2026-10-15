package protocols_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/protocols"
	"github.com/stretchr/testify/require"
)

func report(payload string) []byte { return []byte("\x1b]7501;" + payload + "\a") }
func marker(payload string) []byte { return []byte("\x1b]133;" + payload + "\a") }

func TestPromptCleansActivityAndRetainsOutcomes(t *testing.T) {
	cases := []struct {
		state   string
		outcome status.Outcome
		want    status.State
	}{
		{"idle", status.NoOutcome, status.Withdrawn},
		{"working", status.NoOutcome, status.Withdrawn},
		{"blocked", status.NoOutcome, status.Withdrawn},
		{"done", status.Completed, status.Unknown},
		{"error", status.Failed, status.Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			pane := protocols.New(protocols.Options{ProgramStatus: true})
			pane.Feed(report("state=" + tc.state))

			_, publish := pane.Feed(marker("A;cl=line"))
			pane.ResolvePrompt(true)
			observation := pane.Snapshot()
			pane.Feed(marker("A"))
			pane.ResolvePrompt(true)
			repeated := pane.Snapshot()

			require.False(t, publish, "a marker requires native confirmation before clearing activity")
			require.Equal(t, tc.want, observation.Activity)
			require.Equal(t, tc.outcome, observation.Outcome)
			require.Equal(t, observation, repeated)
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

func TestOrderedDispatchAndSecondaryPrompt(t *testing.T) {
	pane := protocols.New(protocols.Options{ProgramStatus: true})
	input := append(marker("C"), report("state=blocked")...)
	input = append(input, marker("D;0")...)
	input = append(input, marker("A;k=s")...)

	observation, publish := pane.Feed(input)

	require.True(t, publish)
	require.Equal(t, status.Blocked, observation.Activity)
	require.Equal(t, protocols.Finished, observation.Shell.Phase)
	require.True(t, observation.Shell.ExitKnown)
	require.Zero(t, observation.Shell.ExitCode)
	input = append(marker("A"), report("state=working")...)
	observation, publish = pane.Feed(input)
	require.True(t, publish)
	require.Equal(t, status.Working, observation.Activity)
}

func TestResetAndProcessExitHaveDifferentLifetimes(t *testing.T) {
	pane := protocols.New(protocols.Options{ProgramStatus: true})
	pane.Feed(report("state=done"))
	pane.Feed(marker("C"))
	observed := pane.Snapshot().OutcomeSince

	pane.ProcessExited()
	afterExit := pane.Snapshot()
	pane.Reset()
	afterGap := pane.Snapshot()

	require.Equal(t, status.Completed, afterExit.Outcome)
	require.False(t, observed.IsZero())
	require.Equal(t, observed, afterExit.OutcomeSince)
	require.Equal(t, protocols.Shell{}, afterExit.Shell)
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

func TestRISWithdrawsAndDiscardsPartialCommandEnd(t *testing.T) {
	pane := protocols.New(protocols.Options{ProgramStatus: true})
	pane.Feed(report("state=working"))
	pane.Feed(marker("C"))
	pane.Feed([]byte("\x1b]133;D;"))

	observation, publish := pane.Feed([]byte("\x1bc0\a"))
	pane.Feed(marker("D;0"))

	require.True(t, publish)
	require.Equal(t, status.Withdrawn, observation.Activity)
	require.Equal(t, protocols.Shell{}, pane.Snapshot().Shell)
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
		require.Equal(t, protocols.Running, observation.Shell.Phase)
	}
}
