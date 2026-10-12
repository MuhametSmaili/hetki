package profile_test

import (
	"os"
	"strings"
	"testing"

	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/profile"
	"github.com/stretchr/testify/require"
)

// Reduced footer fixtures from the archived isolated Kiro 2.28.0 TUI/V2 probe.
const workingScreen = "ᗣ Thinking... (esc to cancel)\n\n──────────────────\nprobe · claude-haiku-4.5 · ◔ 3%\n/fixture\n\n›  Kiro is working · 1s · Type to steer · Ctrl+S to queue\n\n\n"
const idleScreen = "• Cancelled streaming\n──────────────────\nprobe · claude-haiku-4.5 · ◔ 3%\n/fixture\n\n›  ask a question or describe a task ↵     \n                        /copy to clipboard\n\n"
const approvalScreen = "↓ Shell printf fixture\n\n──────────────────\n shell requires approval       \n ❯ Yes, single permission\n   Trust, always allow in this session\n   No (Tab to edit)\n──────────────────\n esc to close · ↑↓ to navigate · ↵ to select · Tab to edit\n\n"

func TestShippedKiroProfile(t *testing.T) {
	data, err := os.ReadFile("../../../integrations/pane-status/profiles/kiro-2.28.yaml")
	require.NoError(t, err)
	p, err := profile.Load(strings.NewReader(string(data)))
	require.NoError(t, err)
	require.Equal(t, "kiro-cli", p.Command)
	cases := []struct {
		name string
		text string
		want status.State
	}{
		{"working", workingScreen, status.Working},
		{"approval", approvalScreen, status.Blocked},
		{"cancelled ready", idleScreen, status.Idle},
		{"empty", "", status.Unknown},
		{"typed composer", strings.Replace(idleScreen, "ask a question or describe a task ↵", "an unfinished prompt", 1), status.Unknown},
		{"overlay", idleScreen + "Choose a model\n", status.Unknown},
		{"old approval", approvalScreen + workingScreen, status.Working},
		{"quoted working", workingScreen + idleScreen, status.Idle},
		{"old footer", workingScreen + strings.Repeat("different screen\n", 14), status.Unknown},
		{"missing heading", strings.Replace(approvalScreen, " shell requires approval", "", 1), status.Unknown},
		{"wrapped working", strings.Replace(workingScreen, " · Type to steer", "\n · Type to steer", 1), status.Unknown},
		{"wrapped approval", strings.Replace(approvalScreen, " · ↵ to select", "\n · ↵ to select", 1), status.Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := p.Screen.Evaluate(tc.text)

			require.NoError(t, err)
			require.Equal(t, tc.want, state.State)
		})
	}
}
