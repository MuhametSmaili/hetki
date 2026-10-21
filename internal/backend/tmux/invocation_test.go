package tmux

import (
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/stretchr/testify/require"
)

func TestInvocationContextDistinguishesOutsideAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, tmux, pane string
		want             *backend.Invocation
	}{
		{name: "outside"},
		{name: "pane without session environment", pane: "%2", want: &backend.Invocation{}},
		{name: "popup", tmux: "/tmp/test,123,4", want: &backend.Invocation{SessionID: "$4"}},
		{name: "pane", tmux: "/tmp/test,123,4", pane: "%5", want: &backend.Invocation{SessionID: "$4", PaneID: "%5"}},
		{name: "malformed session", tmux: "unknown", want: &backend.Invocation{}},
		{name: "malformed pane", tmux: "/tmp/test,123,4", pane: "unknown", want: &backend.Invocation{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.tmux)
			t.Setenv("TMUX_PANE", tc.pane)

			invocation := invocationContext()

			require.Equal(t, tc.want, invocation)
		})
	}
}
