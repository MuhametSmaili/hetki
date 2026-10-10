package observe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaneProcessesRequireMatchingTerminalAndForegroundGroup(t *testing.T) {
	cases := []struct {
		name   string
		leader processInfo
	}{
		{"wrong group", processInfo{Process: Process{PID: 200, Birth: "b"}, group: 201, foreground: 200, terminal: 1}},
		{"lost foreground", processInfo{Process: Process{PID: 200, Birth: "b"}, group: 200, foreground: 201, terminal: 1}},
		{"wrong terminal", processInfo{Process: Process{PID: 200, Birth: "b"}, group: 200, foreground: 200, terminal: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sample := processSample{cache: make(map[int]processResult)}
			sample.read = func(context.Context, int) (processInfo, error) { return tc.leader, nil }
			sample.cache[100] = processResult{info: processInfo{
				Process: Process{PID: 100, Birth: "root"}, group: 100, foreground: 200, terminal: 1,
			}}

			root, foreground, err := sample.paneProcesses(t.Context(), 100)

			require.Error(t, err)
			require.Equal(t, "100:root", root.Identity())
			require.Empty(t, foreground.Identity())
		})
	}
}

func TestInventoryRetainsStableRootWhenForegroundUnavailable(t *testing.T) {
	missing := errors.New("leader exited")
	s := testSampler(t, func(context.Context, int, ...string) (string, error) {
		return testInventoryOutput(testPaneRow), nil
	})
	s.process = func(ctx context.Context, pid int) (processInfo, error) {
		if pid == 200 {
			return processInfo{}, missing
		}
		info, err := fixtureProcess(ctx, pid)
		if pid == 100 {
			info.foreground = 200
		}
		return info, err
	}

	inventory, err := s.Inventory(t.Context(), 1)
	pane, _ := inventory.Lookup("%1")

	require.NoError(t, err)
	require.ErrorIs(t, inventory.Problem("%1"), missing)
	require.Equal(t, "100:birth-100", pane.Root.Identity())
	require.Empty(t, pane.Foreground.Identity())
	require.False(t, pane.Dead)
}

func TestLinuxProcessStatTerminalFacts(t *testing.T) {
	cases := []struct {
		name, group, terminal, foreground string
		valid                             bool
	}{
		{"terminal foreground", "42", "34816", "50", true},
		{"without terminal", "42", "0", "-1", true},
		{"negative group", "-2", "34816", "50", false},
		{"invalid foreground", "42", "34816", "-2", false},
		{"invalid terminal", "42", "broken", "50", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := strings.Fields("S " + strings.Repeat("0 ", 18) + "9876")
			fields[2], fields[4], fields[5] = tc.group, tc.terminal, tc.foreground
			data := "42 (process (name)) " + strings.Join(fields, " ")

			info, err := parseProcessStat(data, 42)

			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 42, info.group)
			require.Equal(t, "9876", info.Birth)
			if tc.terminal == "0" {
				require.Zero(t, info.terminal)
				require.Equal(t, -1, info.foreground)
			} else {
				require.Equal(t, uint64(34817), info.terminal)
				require.Equal(t, 50, info.foreground)
			}
		})
	}
}
