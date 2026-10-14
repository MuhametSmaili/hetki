package tui

import (
	"testing"

	"github.com/MSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestLiveAdapterHidesOnlyDefaultShellCommandLabels(t *testing.T) {
	for _, test := range []struct {
		name, shell, command, program, want string
		dead                                bool
	}{
		{name: "shell basename", shell: "/bin/zsh", command: "zsh"},
		{name: "shell path", shell: "/bin/zsh", command: "/usr/local/bin/zsh"},
		{name: "different shell", shell: "/bin/zsh", command: "bash", want: "bash"},
		{name: "missing shell", command: "zsh", want: "zsh"},
		{name: "both missing", want: "unknown"},
		{name: "program hint", shell: "/bin/zsh", command: "zsh", program: "task", want: "task"},
		{name: "exited shell", shell: "/bin/zsh", command: "zsh", program: "task", dead: true, want: "exited"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := liveState()
			state.Sessions[0].DefaultShell = test.shell
			window := &state.Sessions[0].Windows[0]
			window.Name = "zsh"
			window.Panes[0].Command = test.command
			window.Panes[0].Program = test.program
			window.Panes[0].Dead = test.dead
			stub := &stubBackend{state: state}
			adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, nil, nil)

			snapshot, err := adapter.Load(t.Context())

			require.NoError(t, err)
			require.Len(t, snapshot.Items, 1)
			require.Equal(t, test.want, snapshot.Items[0].Trailing)
			require.Contains(t, snapshot.Items[0].Primary, "zsh")
			require.Equal(t, test.command, window.Panes[0].Command)
			if test.command != "" {
				require.Contains(t, snapshot.Items[0].SearchFields, list.SearchField{Tier: list.SearchPrimary, Text: test.command})
			}
		})
	}
}
