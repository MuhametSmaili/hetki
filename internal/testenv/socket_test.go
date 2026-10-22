package testenv_test

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestSocketDirSupportsUnixSocketsAndCleansUp(t *testing.T) {
	longTemp := filepath.Join(t.TempDir(), strings.Repeat("long", 30))
	require.NoError(t, os.Mkdir(longTemp, 0700))
	t.Setenv("TMPDIR", longTemp)
	var dirs []string

	t.Run("socket owners", func(t *testing.T) {
		dirs = []string{testenv.SocketDir(t), testenv.SocketDir(t)}
		require.NotEqual(t, dirs[0], dirs[1])

		for _, dir := range dirs {
			info, err := os.Stat(dir)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0700), info.Mode().Perm())

			listener, err := net.Listen("unix", filepath.Join(dir, "tmux.sock"))
			require.NoError(t, err)
			t.Cleanup(func() {
				require.DirExists(t, dir, "socket cleanup must run before directory removal")
				require.NoError(t, listener.Close())
			})
			require.NoError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte("fixture"), 0600))
		}
	})

	for _, dir := range dirs {
		_, err := os.Stat(dir)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}
