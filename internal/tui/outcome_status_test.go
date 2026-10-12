package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/profile"
	"github.com/MSmaili/hetki/internal/tui/list"
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
				bindings["window"] = append(bindings["window"], pane)
			}
			snapshot, err := status.NewSnapshot(values)
			require.NoError(t, err)

			got := bindings.ValueAt("window", snapshot, now)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestOutcomeSchedulesOnlyValidationExpiry(t *testing.T) {
	now := time.Now()
	m := paneStatusModel(t, status.Unknown, now.Add(time.Minute))
	pane := m.paneStatus.options.Bindings["window"][0]
	values, err := status.NewSnapshot(map[status.PaneRef]status.Value{
		pane: {State: status.Unknown, Outcome: status.Completed, ValidUntil: now.Add(time.Minute)},
	})
	require.NoError(t, err)
	m.paneStatus.values = values

	due := m.nextStatusFrame(now)
	current := m.paneStatus.options.Bindings.ValueAt("window", values, now)
	expired := m.paneStatus.options.Bindings.ValueAt("window", values, due)

	require.Equal(t, now.Add(time.Minute), due)
	require.Equal(t, status.Completed, current.Outcome)
	require.Equal(t, status.NoOutcome, expired.Outcome)
	glyph, _ := statusIndicator(expired, due)
	require.Equal(t, "·", glyph)
	require.True(t, m.nextStatusFrame(due).IsZero())
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
			bindings := StatusBindings{list.ItemID("kiro"): {pane}}

			value := bindings.ValueAt("kiro", values, now)
			glyph, _ := statusIndicator(value, now)

			require.Equal(t, tc.glyph, glyph)
			require.Equal(t, status.NoOutcome, value.Outcome)
		})
	}
}
