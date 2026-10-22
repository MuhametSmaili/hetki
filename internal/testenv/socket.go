package testenv

import (
	"os"
	"testing"
)

// SocketDir creates a private, short directory for Unix sockets and removes it
// at test cleanup. Register socket/server cleanup after calling SocketDir so it
// runs before directory removal.
func SocketDir(t testing.TB) string {
	t.Helper()

	//nolint:usetesting // t.TempDir can exceed the Unix socket path limit on macOS
	dir, err := os.MkdirTemp("/tmp", "ht-sock-")
	if err != nil {
		t.Fatalf("create socket directory: %v", err)
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove socket directory %q: %v", dir, err)
		}
	})
	return dir
}
