package tui

import (
	"testing"

	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
	"github.com/stretchr/testify/require"
)

func TestLiveAdapterHidesDefaultShellCommandsFromLabelsAndSearch(t *testing.T) {
	for _, test := range []struct {
		name, shell, command, program, windowName, want, query string
		dead, matches                                          bool
	}{
		{name: "shell basename", shell: "/bin/zsh", command: "zsh", query: "zsh"},
		{name: "shell path", shell: "/bin/zsh", command: "/usr/local/bin/zsh", query: "zsh"},
		{name: "different shell", shell: "/bin/zsh", command: "bash", want: "bash", query: "bash", matches: true},
		{name: "missing shell", command: "zsh", want: "zsh", query: "zsh", matches: true},
		{name: "both missing", want: "unknown", query: "zsh"},
		{name: "program hint", shell: "/bin/zsh", command: "zsh", program: "task", want: "task", query: "zsh"},
		{name: "visible program hint", shell: "/bin/zsh", command: "zsh", program: "task", want: "task", query: "task", matches: true},
		{name: "exited shell", shell: "/bin/zsh", command: "zsh", program: "task", dead: true, want: "exited", query: "zsh"},
		{name: "visible shell window", shell: "/bin/zsh", command: "zsh", windowName: "zsh", query: "zsh", matches: true},
		{name: "visible shell program", shell: "/bin/zsh", command: "zsh", program: "zsh", want: "zsh", query: "zsh", matches: true},
	} {
		for _, projection := range []struct {
			name string
			kind ui.ViewMode
		}{{"flat", ui.ViewFlat}, {"tree", ui.ViewTree}} {
			t.Run(test.name+"/"+projection.name, func(t *testing.T) {
				state := liveState()
				state.Sessions[0].DefaultShell = test.shell
				window := &state.Sessions[0].Windows[0]
				window.Name = "editor"
				if test.windowName != "" {
					window.Name = test.windowName
				}
				window.Panes[0].Command = test.command
				window.Panes[0].Program = test.program
				window.Panes[0].Dead = test.dead
				stub := &stubBackend{state: state}
				adapter := newLiveAdapter(func() (Backend, error) { return stub, nil }, nil, nil)

				snapshot, err := adapter.Load(t.Context(), projection.kind)

				require.NoError(t, err)
				require.Len(t, snapshot.Items, 1)
				item := snapshot.Items[0]
				if projection.kind == ui.ViewTree {
					require.Len(t, item.Children, 1)
					item = item.Children[0]
				}
				require.Equal(t, test.want, item.Trailing)
				require.Contains(t, item.Primary, window.Name)
				require.Equal(t, test.command, window.Panes[0].Command)

				model, err := list.New(snapshot)
				require.NoError(t, err)

				model.SetQuery(test.query)

				require.Equal(t, test.matches, len(model.Rows()) > 0)
			})
		}
	}
}
