package statusobserver

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Lifecycle regressions must not re-execute the test suite as a detached daemon.
	if len(os.Args) > 1 && os.Args[1] == DaemonCommand {
		fmt.Fprintln(os.Stderr, "unexpected status daemon launch from the test executable")
		os.Exit(2)
	}

	os.Exit(m.Run())
}
