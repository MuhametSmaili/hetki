package profile_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/status/profile"
	"github.com/stretchr/testify/require"
)

func writeProfile(t *testing.T, dir, name, text string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), 0600))
}

func TestCatalogMatchesExactCommandsAndReturnsCopies(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "screen.yaml", screenYAML)
	writeProfile(t, dir, "explicit.yml", protocolYAML)
	writeProfile(t, dir, "README.md", "ignored")
	catalog, err := profile.LoadDirectory(t.Context(), dir)
	require.NoError(t, err)

	p, found := catalog.Match("fixture")
	p.ID = "changed"
	stored, stillFound := catalog.Match("fixture")
	_, unmatched := catalog.Match("Fixture")
	explicit, explicitFound := catalog.Match("explicit")

	require.True(t, found)
	require.True(t, stillFound)
	require.Equal(t, "fixture", stored.ID)
	require.NotEqual(t, p.ID, stored.ID)
	require.False(t, unmatched)
	require.True(t, explicitFound)
	require.Equal(t, profile.Protocol, explicit.Source)
}

func TestCatalogMissingDirectoryHasNoApplicationClaim(t *testing.T) {
	catalog, err := profile.LoadDirectory(t.Context(), filepath.Join(t.TempDir(), "missing"))

	require.NoError(t, err)
	_, found := catalog.Match("opencode")
	require.False(t, found)
}

func TestCatalogRejectsConflictsAtomically(t *testing.T) {
	for name, second := range map[string]string{
		"duplicate command": strings.Replace(screenYAML, "id: fixture", "id: other", 1),
		"duplicate id":      strings.Replace(screenYAML, "command: fixture", "command: other", 1),
		"invalid file":      "version: unsupported\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeProfile(t, dir, "a.yaml", screenYAML)
			writeProfile(t, dir, "b.yaml", second)

			catalog, err := profile.LoadDirectory(t.Context(), dir)

			require.Error(t, err)
			require.Nil(t, catalog)
		})
	}
}

func TestCatalogRejectsNonregularFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "fifo", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "bad.yaml")
			switch kind {
			case "symlink":
				target := filepath.Join(t.TempDir(), "profile.yaml")
				require.NoError(t, os.WriteFile(target, []byte(screenYAML), 0600))
				require.NoError(t, os.Symlink(target, path))
			case "directory":
				require.NoError(t, os.Mkdir(path, 0700))
			case "fifo":
				require.NoError(t, syscall.Mkfifo(path, 0600))
			case "oversized":
				writeProfile(t, dir, "bad.yaml", strings.Repeat(" ", profile.MaxProfileBytes+1))
			}

			catalog, err := profile.LoadDirectory(t.Context(), dir)

			require.Error(t, err)
			require.Nil(t, catalog)
		})
	}
}

func TestCatalogBoundsDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		yaml  bool
	}{
		{"profile limit", profile.MaxProfiles + 1, true},
		{"directory limit", 257, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for i := range tc.count {
				name, text := fmt.Sprintf("%d.txt", i), "ignored"
				if tc.yaml {
					name = fmt.Sprintf("%d.yaml", i)
					text = strings.ReplaceAll(protocolYAML, "explicit", fmt.Sprintf("app%d", i))
				}
				writeProfile(t, dir, name, text)
			}

			catalog, err := profile.LoadDirectory(t.Context(), dir)

			require.Error(t, err)
			require.Nil(t, catalog)
		})
	}
}

func TestCatalogHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := profile.LoadDirectory(ctx, t.TempDir())

	require.ErrorIs(t, err, context.Canceled)
}

func TestDefaultDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err := profile.DefaultDirectory()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".config", "hetki", "status.d"), dir)

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir, err = profile.DefaultDirectory()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(xdg, "hetki", "status.d"), dir)

	t.Setenv("XDG_CONFIG_HOME", "relative")
	_, err = profile.DefaultDirectory()
	require.Error(t, err)
}
