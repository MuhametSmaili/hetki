package tui

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestTrackedScopeQueryEditsObserveChangedBindings(t *testing.T) {
	m, now := scopeFixture(t, "flat")
	m.scope.mode = ScopeTracked
	m.applyScope("idle", now)
	require.Equal(t, []list.ItemID{{ID: "idle"}}, m.items.RowIDs())
	pane := m.paneStatus.options.Bindings[list.ItemID{ID: "idle"}]
	m.applyStatusUpdate(StatusUpdate{
		Values: m.paneStatus.values, Bindings: StatusBindings{{ID: "shell"}: pane},
	})

	m.setQuery("working")

	require.Equal(t, []list.ItemID{{ID: "shell"}}, m.items.RowIDs())
	m.setQuery("")
	require.Equal(t, []list.ItemID{{ID: "shell"}}, m.items.RowIDs())
}

func TestTrackedScopeExpiresHiddenRows(t *testing.T) {
	for _, trigger := range []string{"query", "frame"} {
		t.Run(trigger, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m, _ := scopeFixture(t, "flat")
				now := time.Now()
				pane := m.paneStatus.options.Bindings[list.ItemID{ID: "idle"}][0]
				value := status.Value{
					State: status.Unknown, Outcome: status.Completed, OutcomeEnded: true,
					OutcomeSince: now, ValidUntil: now.Add(time.Hour),
				}
				values, err := status.NewSnapshot(map[status.PaneRef]status.Value{pane: value})
				require.NoError(t, err)
				m.applyStatusUpdate(StatusUpdate{Values: values})
				m.scope.mode = ScopeTracked
				m.setQuery("no-match")
				due := value.NextChange(now)
				require.False(t, due.IsZero())
				require.Equal(t, due, m.nextStatusFrame(now), "hidden membership must schedule expiry")
				m, _ = m.syncStatusFrame(now)
				generation := m.paneStatus.frameGeneration

				time.Sleep(due.Sub(now))
				if trigger == "frame" {
					m, _ = updateModel(t, m, statusFrameMsg{generation: generation})
					require.True(t, m.scope.changeDue.IsZero())
				}
				m.setQuery("")

				require.Empty(t, m.items.RowIDs())
				require.True(t, m.nextStatusFrame(time.Now()).IsZero())
			})
		})
	}
}
