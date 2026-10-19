package update

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubMise(t *testing.T, output string) string {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n[ \"$*\" = 'ls --installed --json go:github.com/MuhametSmaili/hetki' ] || exit 99\nprintf '%s' \"$HETKI_TEST_MISE_OUTPUT\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "mise"), []byte(script), 0755))
	t.Setenv("HETKI_TEST_MISE_OUTPUT", output)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(bin, "mise")
}

func miseInstallation(t *testing.T) (string, map[string]any) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	exe := filepath.Join(root, "custom-data", "tool", "main", "bin", "hetki")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0700))
	fakeBinary(t, exe, "v1.0.0")
	return exe, map[string]any{
		"version": "main", "requested_version": "main", "active": true, "installed": true,
		"install_path": filepath.Dir(filepath.Dir(exe)),
	}
}

func stubMiseInstalls(t *testing.T, entries ...map[string]any) string {
	t.Helper()
	metadata, err := json.Marshal(entries)
	require.NoError(t, err)
	return stubMise(t, string(metadata))
}

func TestDetermineUpdaterRefusesMiseSelectors(t *testing.T) {
	for _, test := range []struct {
		name    string
		fields  map[string]any
		release bool
		wantErr string
	}{
		{name: "other branch", fields: map[string]any{"version": "dev", "requested_version": "dev"}, wantErr: "active main"},
		{name: "release", fields: map[string]any{"version": "v1.0.0", "requested_version": "v1.0.0"}, wantErr: "active main"},
		{name: "pinned commit", fields: map[string]any{"version": testCommit, "requested_version": testCommit}, wantErr: "active main"},
		{name: "aliased request", fields: map[string]any{"requested_version": "latest"}, wantErr: "active main"},
		{name: "missing request", fields: map[string]any{"requested_version": nil}, wantErr: "active main"},
		{name: "inactive main", fields: map[string]any{"active": false}, wantErr: "active main"},
		{name: "not installed", fields: map[string]any{"installed": false}, wantErr: "active main"},
		{name: "stable update", release: true, wantErr: "use mise for release updates"},
	} {
		t.Run(test.name, func(t *testing.T) {
			exe, entry := miseInstallation(t)
			for key, value := range test.fields {
				entry[key] = value
			}
			stubMiseInstalls(t, entry)
			t.Setenv("GOBIN", filepath.Dir(exe))

			updater, err := DetermineUpdater(t.Context(), exe, Options{Head: !test.release})

			require.ErrorContains(t, err, test.wantErr)
			assert.Nil(t, updater)
			assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
		})
	}
}

func TestDetermineUpdaterRejectsMiseLinks(t *testing.T) {
	for _, component := range []string{"", "bin", "bin/hetki"} {
		t.Run("linked/"+component, func(t *testing.T) {
			exe, entry := miseInstallation(t)
			path := filepath.Join(filepath.Dir(filepath.Dir(exe)), component)
			require.NoError(t, os.Rename(path, path+".real"))
			require.NoError(t, os.Symlink(path+".real", path))
			stubMiseInstalls(t, entry)

			updater, err := DetermineUpdater(t.Context(), exe, Options{Head: true})

			require.ErrorContains(t, err, "linked installations")
			assert.Nil(t, updater)
			assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
		})
	}
}

func TestDetermineUpdaterRejectsAmbiguousMiseOwnership(t *testing.T) {
	exe, entry := miseInstallation(t)
	stubMiseInstalls(t, entry, map[string]any{
		"version": "dev", "installed": true, "install_path": entry["install_path"],
	})

	updater, err := DetermineUpdater(t.Context(), exe, Options{Head: true})

	require.ErrorContains(t, err, "multiple mise installations")
	assert.Nil(t, updater)
}

func TestDetermineUpdaterRejectsUnknownMiseOwnership(t *testing.T) {
	for _, test := range []struct{ name, output, wantErr string }{
		{name: "not listed", output: `[]`, wantErr: "unsupported installation"},
		{name: "invalid json", output: `{`, wantErr: "decoding mise installations"},
		{name: "no metadata", output: `null`, wantErr: "no installation metadata"},
		{name: "wrong shape", output: `{}`, wantErr: "decoding mise installations"},
		{name: "missing path", output: `[{"version":"main"}]`, wantErr: "invalid installation path"},
		{name: "relative path", output: `[{"install_path":"main"}]`, wantErr: "invalid installation path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			exe, _ := miseInstallation(t)
			stubMise(t, test.output)

			updater, err := DetermineUpdater(t.Context(), exe, Options{Head: true})

			require.ErrorContains(t, err, test.wantErr)
			assert.Nil(t, updater)
		})
	}
}

func TestDetermineUpdaterRejectsFailedMiseQuery(t *testing.T) {
	exe, entry := miseInstallation(t)
	mise := stubMiseInstalls(t, entry)
	script, err := os.ReadFile(mise)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(mise, append(script, []byte("exit 7\n")...), 0755))

	updater, err := DetermineUpdater(t.Context(), exe, Options{Head: true})

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 7, exitErr.ExitCode())
	assert.Nil(t, updater)
}

func TestDetermineUpdaterCancelsMiseDiscovery(t *testing.T) {
	exe, _ := miseInstallation(t)
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "mise"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0755))
	t.Setenv("PATH", bin)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	updater, err := DetermineUpdater(ctx, exe, Options{Head: true})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, updater)
}

func TestServiceMiseHeadDryRunKeepsInstallation(t *testing.T) {
	exe, entry := miseInstallation(t)
	t.Setenv("PATH", t.TempDir())
	stubMiseInstalls(t, entry)
	var output bytes.Buffer
	previousOutput := logger.SetOutput(&output)
	t.Cleanup(func() { logger.SetOutput(previousOutput) })
	service := Service{
		Executable:  func() (string, error) { return exe, nil },
		ResolveHead: func(context.Context) (string, error) { return testCommit, nil },
	}

	err := service.Run(t.Context(), Options{Head: true, DryRun: true, CurrentVersion: "v1.0.0"})

	require.NoError(t, err)
	assert.Contains(t, output.String(), modulePath+"@"+testCommit)
	assert.Contains(t, output.String(), exe)
	assert.NotContains(t, output.String(), "@main")
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
}

func TestDetermineUpdaterAcceptsMiseMain(t *testing.T) {
	exe, entry := miseInstallation(t)
	stubMiseInstalls(t, entry)

	updater, err := DetermineUpdater(t.Context(), exe, Options{Head: true, Verbose: true})

	require.NoError(t, err)
	assert.Equal(t, &GoUpdater{exePath: exe, Verbose: true}, updater)
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
}
