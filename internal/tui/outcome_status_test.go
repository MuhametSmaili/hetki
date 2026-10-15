package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/profile"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestWindowOutcomePriority(t *testing.T) {
	now := time.Now()
	success := status.Value{State: status.Unknown, Outcome: status.Completed}
	failure := status.Value{State: status.Unknown, Outcome: status.Failed}
	unknown := status.Value{State: status.Unknown}
	working := status.Value{State: status.Working}
	blocked := status.Value{State: status.Blocked}
	idle := status.Value{State: status.Idle}
	cases := []struct {
		name   string
		values []status.Value
		want   status.Value
	}{
		{"success before idle", []status.Value{idle, success}, success},
		{"unknown before success", []status.Value{success, unknown}, unknown},
		{"error before unknown", []status.Value{unknown, failure, success}, failure},
		{"working before completed results", []status.Value{failure, working, success}, working},
		{"needs input before working", []status.Value{working, blocked, failure}, blocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[status.PaneRef]status.Value)
			bindings := StatusBindings{}
			for i, value := range tc.values {
				pane := status.PaneRef{Server: "server", ID: string(rune('a' + i)), Generation: "birth"}
				value.ValidUntil = now.Add(time.Minute)
				values[pane] = value
				bindings[list.ItemID{ID: "window"}] = append(bindings[list.ItemID{ID: "window"}], pane)
			}
			snapshot, err := status.NewSnapshot(values)
			require.NoError(t, err)

			got := bindings.ValueAt(list.ItemID{ID: "window"}, snapshot, now)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestSuccessBecomesIdleAtFiveSecondsAndStillExpires(t *testing.T) {
	now := time.Now()
	m := paneStatusModel(t, status.Unknown, now.Add(time.Minute))
	pane := m.paneStatus.options.Bindings[list.ItemID{ID: "window"}][0]
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Unknown, Outcome: status.Completed, OutcomeSince: now, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	m.paneStatus.values = values

	due := m.nextStatusFrame(now)
	current := m.paneStatus.options.Bindings.ValueAt(list.ItemID{ID: "window"}, values, due.Add(-time.Nanosecond))
	idle := m.paneStatus.options.Bindings.ValueAt(list.ItemID{ID: "window"}, values, due)

	require.Equal(t, now.Add(5*time.Second), due)
	require.Equal(t, status.Completed, current.Outcome)
	require.Equal(t, status.Value{State: status.Idle}, idle)
	glyph, _ := statusIndicator(idle, due)
	require.Equal(t, "○", glyph)
	require.Equal(t, now.Add(time.Minute), m.nextStatusFrame(due))

	due = now.Add(time.Minute)
	expired := m.paneStatus.options.Bindings.ValueAt(list.ItemID{ID: "window"}, values, due)
	require.Equal(t, status.NoOutcome, expired.Outcome)
	glyph, _ = statusIndicator(expired, due)
	require.Equal(t, "·", glyph)
	require.True(t, m.nextStatusFrame(due).IsZero())
}

func TestCompletionPresentationRespectsAgeAndFreshness(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		outcome status.Outcome
		since   time.Time
		until   time.Time
		want    status.Value
		next    time.Time
	}{
		{"picker opened after success interval", status.Completed, now.Add(-10 * time.Second), now.Add(time.Minute), status.Value{State: status.Idle}, now.Add(time.Minute)},
		{"failure remains visible", status.Failed, now.Add(-time.Hour), now.Add(time.Minute), status.Value{State: status.Unknown, Outcome: status.Failed}, now.Add(time.Minute)},
		{"missing observation time keeps success", status.Completed, time.Time{}, now.Add(time.Minute), status.Value{State: status.Unknown, Outcome: status.Completed}, now.Add(time.Minute)},
		{"validation expires before success interval", status.Completed, now.Add(-time.Second), now, status.Value{State: status.Unknown}, time.Time{}},
		{"validation expires after success interval", status.Completed, now.Add(-time.Hour), now, status.Value{State: status.Unknown}, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := status.Value{State: status.Unknown, Outcome: tc.outcome, OutcomeSince: tc.since, ValidUntil: tc.until}

			got := displayedStatus(value, now)

			require.Equal(t, tc.want, got)
			require.Equal(t, tc.next, nextStatusChange(value, now))
		})
	}
}

func TestWindowSuccessUsesEachPanesCompletionTime(t *testing.T) {
	now := time.Now()
	old := status.PaneRef{Server: "server", ID: "%1", Generation: "old"}
	recent := status.PaneRef{Server: "server", ID: "%2", Generation: "recent"}
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		old:    {State: status.Unknown, Outcome: status.Completed, OutcomeSince: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute)},
		recent: {State: status.Unknown, Outcome: status.Completed, OutcomeSince: now.Add(-time.Second), ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	bindings := StatusBindings{{ID: "window"}: {old, recent}}

	before := bindings.ValueAt(list.ItemID{ID: "window"}, values, now)
	after := bindings.ValueAt(list.ItemID{ID: "window"}, values, now.Add(4*time.Second))

	require.Equal(t, status.Completed, before.Outcome)
	require.Equal(t, status.Value{State: status.Idle}, after)
}

func TestKiroScreenProfileUsesSharedIndicators(t *testing.T) {
	data, err := os.ReadFile("../../integrations/pane-status/profiles/kiro-2.28.yaml")
	require.NoError(t, err)
	p, err := profile.Load(strings.NewReader(string(data)))
	require.NoError(t, err)
	now := time.Now().Truncate(time.Second)
	cases := []struct {
		name   string
		footer string
		glyph  string
	}{
		{"working", "›  Kiro is working · 1s · Type to steer · Ctrl+S to queue", "⠋"},
		{"approval", " shell requires approval\n ❯ Yes, single permission\n esc to close · ↑↓ to navigate · ↵ to select · Tab to edit", "?"},
		{"ready is idle not success", "›  ask a question or describe a task ↵\n /copy to clipboard", "○"},
		{"unrecognized screen", "Choose a model", "·"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := p.Screen.Evaluate(tc.footer)
			require.NoError(t, err)
			pane := status.PaneRef{Server: "server", ID: "%1", Generation: "birth"}
			values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
				pane: {State: result.State, ValidUntil: now.Add(time.Minute)},
			})
			require.NoError(t, err)
			bindings := StatusBindings{list.ItemID{ID: "kiro"}: {pane}}

			value := bindings.ValueAt(list.ItemID{ID: "kiro"}, values, now)
			glyph, _ := statusIndicator(value, now)

			require.Equal(t, tc.glyph, glyph)
			require.Equal(t, status.NoOutcome, value.Outcome)
		})
	}
}
