package observe

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testPaneRow = "$1|@1|%1|100|0|/dev/pts/1|cat|80|30|0\n"

func testInventoryOutput(rows string) string { return "42|123|3.8\n" + rows }

func testSampler(t *testing.T, run queryRunner) *Sampler {
	t.Helper()
	server := Server{Socket: "/tmp/fixture.sock", PID: 42, Started: 123, Birth: "server-birth", Version: "3.8"}
	s, err := NewSampler(server, run)
	require.NoError(t, err)
	s.process = fixtureProcess
	return s
}

func fixtureProcess(ctx context.Context, pid int) (processInfo, error) {
	birth := fmt.Sprintf("birth-%d", pid)
	if pid == 42 {
		birth = "server-birth"
	}
	return processInfo{Process: Process{PID: pid, Birth: birth}, group: pid, foreground: pid, terminal: 1}, ctx.Err()
}

func TestInventorySharesNativeReadsAndLinkedPaneFacts(t *testing.T) {
	queries := 0
	reads := make(map[int]int)
	rows := testPaneRow + strings.Replace(testPaneRow, "$1|", "$2|", 1)
	s := testSampler(t, func(ctx context.Context, limit int, args ...string) (string, error) {
		queries++
		require.Equal(t, maxInventoryBytes, limit)
		require.Equal(t, []string{"-S", "/tmp/fixture.sock"}, args[:2])
		require.True(t, slices.Contains(args, "list-panes"))
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		return testInventoryOutput(rows), nil
	})
	s.process = func(ctx context.Context, pid int) (processInfo, error) {
		reads[pid]++
		info, err := fixtureProcess(ctx, pid)
		if pid == 100 {
			info.foreground = 200
		}
		return info, err
	}

	inventory, err := s.Inventory(t.Context(), 7)
	pane, found := inventory.Lookup("%1")
	locations := slices.Collect(inventory.Locations())

	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, inventory.Problem("%1"))
	require.Equal(t, uint64(7), inventory.Revision())
	require.False(t, inventory.StartedAt().IsZero())
	require.Len(t, inventory.panes, 1)
	require.Len(t, locations, 2)
	require.Equal(t, 100, pane.Root.PID)
	require.Equal(t, 200, pane.Foreground.PID)
	require.Equal(t, 2, queries)
	require.Equal(t, map[int]int{42: 4, 100: 2, 200: 2}, reads)
	locations[0].Session = "$99"
	pane.Command = "mutated"
	stored, _ := inventory.Lookup("%1")
	require.Equal(t, "cat", stored.Command)
	require.Equal(t, "$1", slices.Collect(inventory.Locations())[0].Session)
}

func TestInventoryPreservesUnavailableVersusDead(t *testing.T) {
	denied := errors.New("native access denied")
	rows := testPaneRow + "$1|@1|%2||1||exited|80|30|0\n"
	s := testSampler(t, func(context.Context, int, ...string) (string, error) {
		return testInventoryOutput(rows), nil
	})
	s.process = func(ctx context.Context, pid int) (processInfo, error) {
		if pid == 100 {
			return processInfo{}, denied
		}
		return fixtureProcess(ctx, pid)
	}

	inventory, err := s.Inventory(t.Context(), 1)
	unavailable, _ := inventory.Lookup("%1")
	dead, _ := inventory.Lookup("%2")

	require.NoError(t, err)
	require.False(t, unavailable.Dead)
	require.ErrorIs(t, inventory.Problem("%1"), denied)
	require.True(t, dead.Dead)
	require.NoError(t, inventory.Problem("%2"))
	require.Zero(t, dead.RootPID)
	_, unavailableErr := inventory.Target("%1", Region{})
	_, deadErr := inventory.Target("%2", Region{})
	require.ErrorIs(t, unavailableErr, denied)
	require.Error(t, deadErr)
}

func TestInventoryRejectsNativeLifetimeChanges(t *testing.T) {
	cases := []struct {
		name string
		pid  int
		want error
	}{
		{"server PID reuse", 42, ErrServerChanged},
		{"root PID reuse", 100, ErrChanged},
		{"foreground PID reuse", 200, ErrChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries := 0
			s := testSampler(t, func(context.Context, int, ...string) (string, error) {
				queries++
				return testInventoryOutput(testPaneRow), nil
			})
			s.process = func(ctx context.Context, pid int) (processInfo, error) {
				info, err := fixtureProcess(ctx, pid)
				if pid == 100 {
					info.foreground = 200
				}
				if queries >= 2 && pid == tc.pid {
					info.Birth = "replacement"
				}
				return info, err
			}

			inventory, err := s.Inventory(t.Context(), 1)

			if tc.pid == 42 {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
				require.ErrorIs(t, inventory.Problem("%1"), tc.want)
				pane, _ := inventory.Lookup("%1")
				require.Empty(t, pane.Foreground.Identity())
			}
		})
	}
}

func TestInventoryRejectsMalformedOrIncompleteQueries(t *testing.T) {
	cases := []struct {
		name string
		out  string
	}{
		{"wrong server", "43|123|3.8\n" + testPaneRow},
		{"truncated row", testInventoryOutput(strings.TrimSuffix(testPaneRow, "\n"))},
		{"duplicate occurrence", testInventoryOutput(testPaneRow + testPaneRow)},
		{"conflicting linked facts", testInventoryOutput(testPaneRow + "$2|@1|%1|101|0|/dev/pts/1|cat|80|30|0\n")},
		{"invalid flag", testInventoryOutput(strings.Replace(testPaneRow, "|0|", "|yes|", 1))},
		{"missing live PID", testInventoryOutput(strings.Replace(testPaneRow, "|100|", "||", 1))},
		{"oversize response", strings.Repeat("x", maxInventoryBytes+1)},
		{"oversize row", testInventoryOutput(strings.Repeat("|", 1025) + "\n")},
		{"invalid geometry", testInventoryOutput(strings.Replace(testPaneRow, "|80|30|", "|0|30|", 1))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testSampler(t, func(context.Context, int, ...string) (string, error) { return tc.out, nil })

			inventory, err := s.Inventory(t.Context(), 1)

			require.Error(t, err)
			require.Zero(t, inventory.Revision())
		})
	}
}

func TestInventoryBoundsLinkedOccurrences(t *testing.T) {
	var rows strings.Builder
	for n := 0; n <= maxLocations; n++ {
		fmt.Fprintf(&rows, "$%d|@1|%%1|100|0|/dev/pts/1|cat|80|30|0\n", n)
	}
	s := testSampler(t, func(context.Context, int, ...string) (string, error) {
		return testInventoryOutput(rows.String()), nil
	})

	_, err := s.Inventory(t.Context(), 1)

	require.Error(t, err)
}

func TestInventoryEnforcesPaneLimitAndCancellation(t *testing.T) {
	var rows strings.Builder
	for n := 0; n <= maxInventoryPanes; n++ {
		fmt.Fprintf(&rows, "$1|@1|%%%d|100|0|/dev/pts/1|cat|80|30|0\n", n)
	}
	calls := 0
	s := testSampler(t, func(context.Context, int, ...string) (string, error) {
		calls++
		return testInventoryOutput(rows.String()), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, boundErr := s.Inventory(t.Context(), 1)
	_, cancelErr := s.Inventory(ctx, 2)

	require.Error(t, boundErr)
	require.ErrorIs(t, cancelErr, context.Canceled)
	require.Equal(t, 1, calls)
}
