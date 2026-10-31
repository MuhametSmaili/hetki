package tui

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNonemptyNoColorIsNormalizedOnlyForPicker(t *testing.T) {
	for _, value := range []string{"1", "0", "false", "anything"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("NO_COLOR", value)
			t.Setenv("CLICOLOR_FORCE", "1")

			environ := monochromeEnvironment()

			require.Contains(t, environ, "NO_COLOR=1")
			require.Contains(t, environ, "CLICOLOR_FORCE=1")
			require.Equal(t, value, os.Getenv("NO_COLOR"))
			require.Len(t, environ, len(os.Environ()))
		})
	}
}

func TestEmptyNoColorDoesNotDisableColors(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	environ := monochromeEnvironment()

	require.Contains(t, environ, "NO_COLOR=")
	require.NotContains(t, environ, "NO_COLOR=1")
}
