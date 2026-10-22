package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverNamedPathSelectsConfiguredWorkspace(t *testing.T) {
	for _, tt := range []struct {
		name     string
		selector string
		files    []string
		want     string
	}{
		{name: "existing yml", selector: "dev", files: []string{"dev.yml"}, want: "dev.yml"},
		{name: "new workspace", selector: "dev", want: "dev.yaml"},
		{name: "yaml takes precedence", selector: "dev", files: []string{"dev.yaml", "dev.yml"}, want: "dev.yaml"},
		{name: "explicit yaml", selector: "dev.yaml", files: []string{"dev.yml"}, want: "dev.yaml"},
		{name: "explicit yml", selector: "dev.yml", files: []string{"dev.yaml"}, want: "dev.yml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			workspaceDir := filepath.Join(configDir, "workspaces")
			require.NoError(t, os.Mkdir(workspaceDir, 0700))
			for _, name := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, name), nil, 0600))
			}
			t.Chdir(t.TempDir())
			for _, name := range []string{"dev", "dev.yaml", "dev.yml", ".hetki.yaml"} {
				require.NoError(t, os.WriteFile(name, nil, 0600))
			}
			resolver := &Resolver{configDir: func() (string, error) { return configDir, nil }}

			path, err := resolver.NamedPath(tt.selector)

			require.NoError(t, err)
			assert.Equal(t, filepath.Join(workspaceDir, tt.want), path)
		})
	}
}

func TestResolverNamedPathKeepsHomeAndNameLiteral(t *testing.T) {
	home := filepath.Join(t.TempDir(), "$HETKI_HOME")
	t.Setenv("HOME", home)
	t.Setenv("HETKI_HOME", "../../other-home")
	t.Setenv("HETKI_NAME", "../../escaped")
	workspaceDir := filepath.Join(home, ".config", "hetki", "workspaces")
	require.NoError(t, os.MkdirAll(workspaceDir, 0700))
	candidate := filepath.Join(workspaceDir, "$HETKI_NAME.yml")
	require.NoError(t, os.WriteFile(candidate, nil, 0600))

	path, err := NewResolver().NamedPath("$HETKI_NAME")

	require.NoError(t, err)
	assert.Equal(t, candidate, path)
}

func TestResolverNamedPathKeepsInvalidCandidatePrecedence(t *testing.T) {
	for _, tt := range []struct {
		name    string
		create  func(string) error
		wantErr string
	}{
		{name: "directory", create: func(path string) error { return os.Mkdir(path, 0700) }, wantErr: "not a regular file"},
		{name: "dangling symlink", create: func(path string) error { return os.Symlink(path+".missing", path) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			workspaceDir := filepath.Join(configDir, "workspaces")
			require.NoError(t, os.Mkdir(workspaceDir, 0700))
			candidate := filepath.Join(workspaceDir, "dev.yaml")
			require.NoError(t, tt.create(candidate))
			require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "dev.yml"), nil, 0600))
			resolver := &Resolver{configDir: func() (string, error) { return configDir, nil }}

			path, err := resolver.NamedPath("dev")

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.ErrorContains(t, err, fmt.Sprintf("%q", candidate))
				assert.Empty(t, path, "an invalid .yaml candidate must not fall back to .yml")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, candidate, path)
		})
	}
}

func TestResolverNamedPathPreservesInspectionErrors(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "workspaces"), nil, 0600))
	resolver := &Resolver{configDir: func() (string, error) { return configDir, nil }}

	path, err := resolver.NamedPath("dev")

	assert.Empty(t, path)
	require.ErrorIs(t, err, syscall.ENOTDIR)
	assert.ErrorContains(t, err, fmt.Sprintf("%q", filepath.Join(configDir, "workspaces", "dev.yaml")))
}

func TestFilePathPreservesDirectoryTraversal(t *testing.T) {
	root := t.TempDir()
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(outside, "child"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "work.yaml"), nil, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "work.yaml"), nil, 0600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "child"), filepath.Join(root, "link")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "work.yaml"), filepath.Join(outside, "alias.yaml")))
	t.Chdir(root)
	t.Setenv("HOME", root)

	for _, test := range []struct {
		name, input, want string
		wantErr           error
	}{
		{"relative", "link/../work.yaml", filepath.Join(outside, "work.yaml"), nil},
		{"absolute", root + "/link/../work.yaml", filepath.Join(outside, "work.yaml"), nil},
		{"home", "~/link/../work.yaml", filepath.Join(outside, "work.yaml"), nil},
		{"new directories after traversal", "link/../new/work.yaml", filepath.Join(outside, "new", "work.yaml"), nil},
		{"final symlink retained", "link/../alias.yaml", filepath.Join(outside, "alias.yaml"), nil},
		{"missing parent", "missing/../work.yaml", "", os.ErrNotExist},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, err := FilePath(test.input)

			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, path)
		})
	}
}

func TestFilePathExpandsFileTargets(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Chdir(directory)
	t.Setenv("HOME", directory)
	t.Setenv("HETKI_FILE", "~/dev.yml")
	want := filepath.Join(directory, "dev.yml")

	for _, input := range []string{"./dev.yml", "~/dev.yml", "$HETKI_FILE", want} {
		t.Run(input, func(t *testing.T) {
			path, err := FilePath(input)

			require.NoError(t, err)
			assert.Equal(t, want, path)
		})
	}
}

func TestFilePathDoesNotReexpandEnvironmentValues(t *testing.T) {
	home := filepath.Join(t.TempDir(), "$HETKI_LITERAL")
	t.Setenv("HOME", home)
	t.Setenv("HETKI_LITERAL", "../../escaped")
	t.Setenv("HETKI_FILE", "~/$HETKI_LITERAL.yml")
	t.Setenv("HETKI_FILENAME", "$HETKI_LITERAL")

	for _, input := range []string{"$HETKI_FILE", "~/$HETKI_FILENAME.yml"} {
		t.Run(input, func(t *testing.T) {
			path, err := FilePath(input)

			require.NoError(t, err)
			assert.Equal(t, filepath.Join(home, "$HETKI_LITERAL.yml"), path)
		})
	}
}

func TestFilePathRejectsDirectoryShapeBeforeCleaning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HETKI_EMPTY", "")
	t.Setenv("HETKI_DIRECTORY", "~/dev.yaml/")

	for _, input := range []string{"~/dev.yaml/", "~/dev.yaml/.", "~/dev.yaml/child/..", "$HETKI_EMPTY", "$HETKI_DIRECTORY"} {
		t.Run(input, func(t *testing.T) {
			path, err := FilePath(input)

			require.Error(t, err)
			assert.Empty(t, path)
			assert.ErrorContains(t, err, fmt.Sprintf("%q", input))
		})
	}
}

func TestResolverNamedPathRejectsEscapeReservedAndInvalidNames(t *testing.T) {
	configCalled := false
	resolver := &Resolver{configDir: func() (string, error) {
		configCalled = true
		return t.TempDir(), nil
	}}

	for _, name := range []string{"", ".", "..", "../dev", "dir/dev", `dir\\dev`, "/tmp/dev", "dev.json", "dev.toml", ".yaml", " dev", "dev ", "dev\nname"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			_, err := resolver.NamedPath(name)

			assert.Error(t, err)
		})
	}
	assert.False(t, configCalled, "invalid names must fail before resolving or touching the workspace directory")
}
