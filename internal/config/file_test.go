package config_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	for _, tt := range []struct {
		name, xdg, home, want string
		invalid               bool
	}{
		{name: "absolute XDG", xdg: xdg, home: home, want: filepath.Join(xdg, "hetki", "config.yaml")},
		{name: "HOME fallback", home: home, want: filepath.Join(home, ".config", "hetki", "config.yaml")},
		{name: "relative XDG ignored", xdg: "relative", home: home, want: filepath.Join(home, ".config", "hetki", "config.yaml")},
		{name: "missing HOME", invalid: true},
		{name: "relative HOME", home: "relative", invalid: true},
		{name: "XDG needs no HOME", xdg: xdg, want: filepath.Join(xdg, "hetki", "config.yaml")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.home)
			t.Setenv("XDG_CONFIG_HOME", tt.xdg)

			got, err := config.DefaultPath()

			if tt.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadFile(t *testing.T) {
	for _, kind := range []string{"regular", "missing", "symlink", "directory", "FIFO", "device", "oversize", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			switch kind {
			case "regular", "symlink":
				require.NoError(t, os.WriteFile(path, []byte("ui: {view: tree}"), 0600))
				if kind == "symlink" {
					link := path + ".link"
					require.NoError(t, os.Symlink(path, link))
					path = link
				}
			case "directory":
				require.NoError(t, os.Mkdir(path, 0700))
			case "FIFO":
				require.NoError(t, syscall.Mkfifo(path, 0600))
			case "device":
				path = "/dev/null"
			case "oversize":
				require.NoError(t, os.WriteFile(path, []byte(strings.Repeat(" ", 65537)), 0600))
			case "invalid":
				require.NoError(t, os.WriteFile(path, []byte("ui: {view: null}"), 0600))
			}

			got, err := config.LoadFile(t.Context(), path)

			switch kind {
			case "regular", "symlink":
				require.NoError(t, err)
				require.Equal(t, ptr("tree"), got.UI.View)
			case "missing":
				require.NoError(t, err)
				require.Equal(t, config.Config{}, got)
				_, err = os.Stat(path)
				require.ErrorIs(t, err, os.ErrNotExist)
			default:
				require.ErrorContains(t, err, path)
				require.Equal(t, config.Config{}, got)
			}
		})
	}
}

func TestLoadFileCanceledBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(t.TempDir(), "missing")

	_, err := config.LoadFile(ctx, path)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, path)
}

func TestLoadFilePreservesFilesystemCause(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop")
	require.NoError(t, os.Symlink(path, path))

	_, err := config.LoadFile(t.Context(), path)

	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.ErrorIs(t, err, syscall.ELOOP)
	require.ErrorContains(t, err, path)
}

func TestDecodePreservesReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("ui: {}"), 0600))
	file, err := os.Open(path)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	_, err = config.Decode(file)

	require.ErrorIs(t, err, os.ErrClosed)
	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	require.Equal(t, path, pathError.Path)
}

func TestDecodeBoundsActualRead(t *testing.T) {
	reader := strings.NewReader(strings.Repeat(" ", 100_000))

	_, err := config.Decode(reader)

	require.ErrorContains(t, err, "64 KiB")
	require.Equal(t, 100_000-65537, reader.Len())
}
