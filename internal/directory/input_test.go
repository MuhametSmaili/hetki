package directory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePathsWithoutShellEvaluation(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	for _, test := range []struct {
		name, value, base, want string
		invalid                 bool
	}{
		{name: "home", value: "~", want: "/home/test"},
		{name: "home child", value: "~/Projects/api", want: "/home/test/Projects/api"},
		{name: "absolute", value: "/work/api/../web/", want: "/work/web"},
		{name: "relative", value: "../web", base: "/work/api", want: "/work/web"},
		{name: "spaces preserved", value: "/work/ space ", want: "/work/ space "},
		{name: "literal shell syntax", value: "$(touch marker);$HOME", base: "/work", want: "/work/$(touch marker);$HOME"},
		{name: "empty", invalid: true},
		{name: "relative without base", value: "api", invalid: true},
		{name: "other user", value: "~someone/api", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Resolve(test.value, test.base)

			if test.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestValidateDirectoryAndSymlinkButRejectFileAndMissingPath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), nil, 0o600))
	require.NoError(t, os.Symlink(root, filepath.Join(root, "link")))
	for _, test := range []struct {
		name    string
		invalid bool
	}{
		{name: "."},
		{name: "link"},
		{name: "file", invalid: true},
		{name: "missing", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, err := Validate(test.name, root)

			if test.invalid {
				require.Error(t, err)
				require.Empty(t, path)
				return
			}
			require.NoError(t, err)
			require.Equal(t, filepath.Join(root, test.name), path)
		})
	}
}

func TestSuggestionsOnlyIncludeMatchingDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"api", "app with spaces", "web", ".hidden"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "another-file"), nil, 0o600))
	require.NoError(t, os.Symlink(filepath.Join(root, "api"), filepath.Join(root, "alias")))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "absent")))
	t.Setenv("HOME", root)
	for _, test := range []struct {
		name, value string
		want        []string
	}{
		{name: "matching prefix", value: "a", want: []string{"alias/", "api/", "app with spaces/"}},
		{name: "hidden only when requested", value: ".", want: []string{".hidden/"}},
		{name: "home", value: "~/a", want: []string{"~/alias/", "~/api/", "~/app with spaces/"}},
		{name: "absolute", value: root + "/a", want: []string{root + "/alias/", root + "/api/", root + "/app with spaces/"}},
		{name: "no match", value: "unknown"},
		{name: "empty lists base", want: []string{"alias/", "api/", "app with spaces/", "web/"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Suggest(t.Context(), test.value, root)

			require.NoError(t, err)
			require.Equal(t, test.want, got.Paths)
			require.False(t, got.Incomplete)
		})
	}
}

func TestSuggestionsAreBoundedAndCancellationIsPreserved(t *testing.T) {
	root := t.TempDir()
	for i := range maxMatches + 2 {
		require.NoError(t, os.Mkdir(filepath.Join(root, fmt.Sprintf("folder-%02d", i)), 0o700))
	}

	got, err := Suggest(t.Context(), "folder-", root)

	require.NoError(t, err)
	require.Len(t, got.Paths, maxMatches)
	require.True(t, got.Incomplete)
	require.Equal(t, "folder-00/", got.Paths[0])
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Suggest(ctx, "folder-", root)
	require.ErrorIs(t, err, context.Canceled)
	_, err = Suggest(t.Context(), "missing/", root)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSuggestionsStopAtScanLimit(t *testing.T) {
	root := t.TempDir()
	for i := range maxEntries + 1 {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%d", i)), nil, 0o600))
	}

	got, err := Suggest(t.Context(), "", root)

	require.NoError(t, err)
	require.Empty(t, got.Paths)
	require.True(t, got.Incomplete)
}

func TestSuggestionsPreserveUnexpectedSymlinkErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "loop")
	require.NoError(t, os.Symlink("loop", path))

	got, err := Suggest(t.Context(), "", root)

	require.ErrorIs(t, err, syscall.ELOOP)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, path, pathErr.Path)
	require.Empty(t, got.Paths)
}

func TestSuggestionsStopBetweenEntriesWhenCanceled(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "first"), 0o700))
	require.NoError(t, os.Symlink("loop", filepath.Join(root, "loop")))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entries[0] = cancelingDirEntry{DirEntry: entries[0], cancel: cancel}
	var got Suggestions

	err = got.addMatches(ctx, root, "", "", entries)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"first/"}, got.Paths)
}

type cancelingDirEntry struct {
	os.DirEntry
	cancel context.CancelFunc
}

func (e cancelingDirEntry) IsDir() bool {
	e.cancel()
	return e.DirEntry.IsDir()
}
