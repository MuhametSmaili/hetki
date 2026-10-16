package statusobserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

func TestDaemonDescriptorRejectsWrongIdentityAndMalformedInput(t *testing.T) {
	server := observe.Server{
		Socket:  "/fixture/tmux.sock",
		PID:     123,
		Started: 456,
		Birth:   "birth",
		Version: "3.8",
	}
	encoded, err := json.Marshal(server)
	require.NoError(t, err)

	cases := []struct {
		name       string
		identity   string
		descriptor string
	}{
		{"wrong server", "other", string(encoded)},
		{"malformed", server.Identity(), "{"},
		{"oversized", server.Identity(), strings.Repeat(" ", maxServerDescriptorBytes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeServer(tc.identity, tc.descriptor)

			require.Error(t, err)
		})
	}
}
