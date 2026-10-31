package tui

import (
	"os"
	"strings"
)

// The pinned colorprofile dependency parses NO_COLOR as a boolean instead of
// treating every nonempty value as enabled. Normalize only this program's copy.
func monochromeEnvironment() []string {
	environ := os.Environ()
	for i, value := range environ {
		if strings.HasPrefix(value, "NO_COLOR=") && value != "NO_COLOR=" {
			environ[i] = "NO_COLOR=1"
		}
	}
	return environ
}
