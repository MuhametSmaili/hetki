package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAction_Args(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   []string
	}{
		{
			name:   "create session",
			action: CreateSession{Name: "dev", WindowName: "editor", Path: "~/code"},
			want:   []string{"new-session", "-d", "-s", "dev", "-n", "editor", "-c", "~/code"},
		},
		{
			name:   "create session no path",
			action: CreateSession{Name: "dev", WindowName: "main"},
			want:   []string{"new-session", "-d", "-s", "dev", "-n", "main"},
		},
		{
			name:   "create window",
			action: CreateWindow{Session: "dev", Name: "editor", Path: "~/code"},
			want:   []string{"new-window", "-t", "dev:", "-n", "editor", "-c", "~/code"},
		},
		{
			name:   "rename session",
			action: RenameSession{Target: "dev", Name: "core"},
			want:   []string{"rename-session", "-t", "dev", "core"},
		},
		{
			name:   "rename window",
			action: RenameWindow{Target: "dev:1", Name: "logs"},
			want:   []string{"rename-window", "-t", "dev:1", "logs"},
		},
		{
			name:   "split pane",
			action: SplitPane{Target: "dev:editor", Path: "~/code"},
			want:   []string{"split-window", "-t", "dev:editor", "-c", "~/code"},
		},
		{
			name:   "send keys",
			action: SendKeys{Target: "dev:editor.0", Keys: "vim"},
			want:   []string{"send-keys", "-l", "-t", "dev:editor.0", "--", "vim", ";", "send-keys", "-t", "dev:editor.0", "Enter"},
		},
		{
			name:   "send keys with leading hyphen",
			action: SendKeys{Target: "%1", Keys: "-foo bar"},
			want:   []string{"send-keys", "-l", "-t", "%1", "--", "-foo bar", ";", "send-keys", "-t", "%1", "Enter"},
		},
		{
			name:   "send literal command separator",
			action: SendKeys{Target: "%1", Keys: ";"},
			want:   []string{"send-keys", "-l", "-t", "%1", "--", `\;`, ";", "send-keys", "-t", "%1", "Enter"},
		},
		{
			name:   "preserve backslash before command separator",
			action: SendKeys{Target: "%1", Keys: `\;`},
			want:   []string{"send-keys", "-l", "-t", "%1", "--", `\\;`, ";", "send-keys", "-t", "%1", "Enter"},
		},
		{
			name:   "send command with trailing semicolon",
			action: SendKeys{Target: "%1", Keys: "printf done;"},
			want:   []string{"send-keys", "-l", "-t", "%1", "--", `printf done\;`, ";", "send-keys", "-t", "%1", "Enter"},
		},
		{
			name:   "preserve backslash before trailing semicolon",
			action: SendKeys{Target: "%1", Keys: `printf done\;`},
			want:   []string{"send-keys", "-l", "-t", "%1", "--", `printf done\\;`, ";", "send-keys", "-t", "%1", "Enter"},
		},
		{
			name:   "kill session",
			action: KillSession{Name: "dev"},
			want:   []string{"kill-session", "-t", "dev"},
		},
		{
			name:   "kill window",
			action: KillWindow{Target: "dev:editor"},
			want:   []string{"kill-window", "-t", "dev:editor"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.action.Args())
		})
	}
}
