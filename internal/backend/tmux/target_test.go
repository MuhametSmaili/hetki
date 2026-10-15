package tmux

import (
	"context"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNavigationAndCaptureRejectMalformedTargetsBeforeDispatch(t *testing.T) {
	b := &TmuxBackend{client: &MockClient{
		RunFunc: func(context.Context, ...string) (string, error) {
			t.Fatal("invalid target must not query tmux")
			return "", nil
		},
		RunLimitedFunc: func(context.Context, int, ...string) (string, error) {
			t.Fatal("invalid target must not capture tmux")
			return "", nil
		},
		ExecuteFunc: func(context.Context, Action) error {
			t.Fatal("invalid target must not navigate tmux")
			return nil
		},
	}}
	tests := []struct {
		name   string
		target backend.Target
	}{
		{"absent", backend.Target{}},
		{"window without session", backend.Target{WindowID: "@1"}},
		{"pane without session", backend.Target{WindowID: "@1", PaneID: "%1"}},
		{"pane without window", backend.Target{SessionID: "$1", PaneID: "%1"}},
		{"bare pane", backend.Target{PaneID: "%1"}},
		{"session name", backend.Target{SessionID: "dev"}},
		{"session index", backend.Target{SessionID: "1"}},
		{"serialized target", backend.Target{SessionID: "$1:@1.%1"}},
		{"window name", backend.Target{SessionID: "$1", WindowID: "editor"}},
		{"window index", backend.Target{SessionID: "$1", WindowID: "0"}},
		{"pane index", backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "0"}},
		{"session leading zero", backend.Target{SessionID: "$01"}},
		{"negative window", backend.Target{SessionID: "$1", WindowID: "@-1"}},
		{"positive pane sign", backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%+1"}},
		{"missing window number", backend.Target{SessionID: "$1", WindowID: "@"}},
		{"non-numeric pane", backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%bad"}},
		{"session whitespace", backend.Target{SessionID: " $1"}},
		{"window newline", backend.Target{SessionID: "$1", WindowID: "@1\n"}},
		{"pane NUL", backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1\x00"}},
		{"window pane syntax", backend.Target{SessionID: "$1", WindowID: "@1.%1"}},
		{"pane command syntax", backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%1;kill-server"}},
		{"session overflow", backend.Target{SessionID: "$9999999999999999999999999"}},
		{"window overflow", backend.Target{SessionID: "$1", WindowID: "@9999999999999999999999999"}},
		{"pane overflow", backend.Target{SessionID: "$1", WindowID: "@1", PaneID: "%9999999999999999999999999"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			switchErr := b.Switch(context.Background(), test.target)
			output, captureErr := b.CapturePane(context.Background(), test.target)

			require.Error(t, switchErr)
			require.Error(t, captureErr)
			assert.Empty(t, output)
		})
	}
}
