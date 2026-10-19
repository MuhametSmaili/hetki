package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func awaitUpdateChannel[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for test channel")
		var zero T
		return zero
	}
}

type stubUpdater struct {
	dryRunCalls int
	updateCalls int
	lastTarget  Target
}

func (s *stubUpdater) Name() string { return "stub" }
func (s *stubUpdater) DryRun(target Target) {
	s.dryRunCalls++
	s.lastTarget = target
}
func (s *stubUpdater) Update(_ context.Context, target Target) error {
	s.updateCalls++
	s.lastTarget = target
	return nil
}

type cancelingReader struct{ cancel context.CancelFunc }

func (r cancelingReader) Read(p []byte) (int, error) {
	p[0] = 'x'
	r.cancel()
	return 1, nil
}

func TestServiceRunCanceledBeforeDispatch(t *testing.T) {
	called := false
	service := Service{Executable: func() (string, error) {
		called = true
		return "", nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.Run(ctx, Options{})

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)
}

func TestDownloadToFileCancelsHTTP(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	file, err := os.CreateTemp(t.TempDir(), "download")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- downloadToFile(ctx, server.URL, file, 1<<20) }()

	awaitUpdateChannel(t, started)
	cancel()

	require.ErrorIs(t, awaitUpdateChannel(t, done), context.Canceled)
	_, err = file.Write(nil)
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestContextReaderStopsHashingWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	_, err := io.Copy(io.Discard, contextReader{ctx: ctx, reader: cancelingReader{cancel: cancel}})

	require.ErrorIs(t, err, context.Canceled)
}

func TestGoUpdaterCancelsProcessTree(t *testing.T) {
	bin := t.TempDir()
	goPath := filepath.Join(bin, "go")
	pidPath := filepath.Join(bin, "child.pid")
	script := "#!/bin/sh\nsleep 10 &\necho $! > \"$HETKI_CHILD_PID\"\nwait\n"
	require.NoError(t, os.WriteFile(goPath, []byte(script), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HETKI_CHILD_PID", pidPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exePath := filepath.Join(bin, "hetki")
	fakeBinary(t, exePath, "v1.0.0")
	done := make(chan error, 1)
	go func() { done <- (&GoUpdater{exePath: exePath}).Update(ctx, testTarget("v1.2.3")) }()
	var earlyErr error
	require.Eventually(t, func() bool {
		select {
		case earlyErr = <-done:
			return true
		default:
		}
		_, err := os.Stat(pidPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, earlyErr, "updater exited before child started")
	require.FileExists(t, pidPath)
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)

	cancel()

	updateErr := awaitUpdateChannel(t, done)
	require.ErrorIs(t, updateErr, context.Canceled)
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}, 5*time.Second, 10*time.Millisecond, "child process %d outlived updater", pid)
}

func TestDirectorySizeExceedsBound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact"), make([]byte, 11), 0600))

	exceeded, err := directorySizeExceeds(dir, 10)
	require.NoError(t, err)
	assert.True(t, exceeded)
	exceeded, err = directorySizeExceeds(dir, 11)
	require.NoError(t, err)
	assert.False(t, exceeded)
}

type vanishedDirEntry struct{ fs.DirEntry }

func (vanishedDirEntry) Type() fs.FileMode          { return 0 }
func (vanishedDirEntry) Info() (fs.FileInfo, error) { return nil, os.ErrNotExist }

func TestRegularFileSizeIgnoresVanishedCacheFile(t *testing.T) {
	size, err := regularFileSize(vanishedDirEntry{})
	require.NoError(t, err)
	assert.Zero(t, size)
}

func TestCommandOutputKillsInheritedPipeDescendant(t *testing.T) {
	for _, test := range []struct{ name, redirect string }{
		{name: "stdout", redirect: "2>/dev/null"},
		{name: "stderr", redirect: ">/dev/null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			script, pidPath := filepath.Join(dir, "leak-output"), filepath.Join(dir, "child.pid")
			body := "#!/bin/sh\nsleep 10 " + test.redirect + " &\necho $! > \"$1\"\necho done\n"
			require.NoError(t, os.WriteFile(script, []byte(body), 0755))
			started := time.Now()

			out, err := commandOutput(context.Background(), time.Second, 1024, script, pidPath)

			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Nil(t, out)
			assert.Less(t, time.Since(started), 4*time.Second)
			pidBytes, readErr := os.ReadFile(pidPath)
			require.NoError(t, readErr)
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
			require.NoError(t, parseErr)
			require.Eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }, time.Second, 10*time.Millisecond)
		})
	}
}

func TestServiceRunDryRunUsesUpdaterDryRun(t *testing.T) {
	updater := &stubUpdater{}
	service := Service{
		SetVerbose:       func(bool) {},
		Executable:       func() (string, error) { return "/tmp/hetki", nil },
		DetermineUpdater: func(string) (Updater, error) { return updater, nil },
		ResolveTarget:    func(context.Context, Options) (string, error) { return "v1.2.3", nil },
		ResolveCommit:    func(context.Context, string) (string, error) { return testCommit, nil },
	}

	err := service.Run(context.Background(), Options{CurrentVersion: "v1.0.0", DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, 1, updater.dryRunCalls)
	assert.Zero(t, updater.updateCalls)
}

func TestServiceRunSkipsUpdateWhenAlreadyLatest(t *testing.T) {
	updater := &stubUpdater{}
	service := Service{
		SetVerbose:       func(bool) {},
		Executable:       func() (string, error) { return "/tmp/hetki", nil },
		DetermineUpdater: func(string) (Updater, error) { return updater, nil },
		ResolveTarget:    func(context.Context, Options) (string, error) { return "v1.2.3", nil },
	}

	err := service.Run(context.Background(), Options{CurrentVersion: "v1.2.3"})
	require.NoError(t, err)
	assert.Zero(t, updater.dryRunCalls)
	assert.Zero(t, updater.updateCalls)
	assert.Empty(t, updater.lastTarget.Tag)
}

func TestServiceRunFailsClosedWhenResolutionFails(t *testing.T) {
	updater := &stubUpdater{}
	service := Service{
		SetVerbose:       func(bool) {},
		Executable:       func() (string, error) { return "/tmp/hetki", nil },
		DetermineUpdater: func(string) (Updater, error) { return updater, nil },
		ResolveTarget: func(context.Context, Options) (string, error) {
			return "", errors.New("network unreachable")
		},
	}

	err := service.Run(context.Background(), Options{CurrentVersion: "v1.0.0"})
	require.ErrorContains(t, err, "could not resolve a release")
	assert.Zero(t, updater.updateCalls)
}

func TestServiceRunExactVersionReinstallsAndDowngrades(t *testing.T) {
	updater := &stubUpdater{}
	service := Service{
		SetVerbose:       func(bool) {},
		Executable:       func() (string, error) { return "/tmp/hetki", nil },
		DetermineUpdater: func(string) (Updater, error) { return updater, nil },
		ResolveTarget: func(_ context.Context, opts Options) (string, error) {
			assert.Equal(t, "v1.0.0", opts.TargetVersion)
			return "v1.0.0", nil
		},
		ResolveCommit: func(context.Context, string) (string, error) { return testCommit, nil },
	}

	// Downgrade and reinstall are the same explicit path (D4).
	err := service.Run(context.Background(), Options{CurrentVersion: "v1.2.3", TargetVersion: "v1.0.0"})
	require.NoError(t, err)
	assert.Equal(t, 1, updater.updateCalls)
	assert.Equal(t, testTarget("v1.0.0"), updater.lastTarget)
}

func TestServiceRunRefusesPrerelease(t *testing.T) {
	updater := &stubUpdater{}
	service := Service{
		SetVerbose:       func(bool) {},
		Executable:       func() (string, error) { return "/tmp/hetki", nil },
		DetermineUpdater: func(string) (Updater, error) { return updater, nil },
		ResolveTarget:    func(context.Context, Options) (string, error) { return "v1.3.0-rc.1", nil },
	}

	err := service.Run(context.Background(), Options{CurrentVersion: "v1.2.3"})
	require.ErrorContains(t, err, "prerelease")
	assert.Zero(t, updater.updateCalls)
}

func TestSourceBuildOverridesPersistedChecksumExclusions(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "goenv")
	require.NoError(t, os.WriteFile(config, []byte("GOSUMDB=off\nGOPROXY=off\nGONOSUMDB=*\nGOPRIVATE=*\nGONOPROXY=*\n"), 0600))
	t.Setenv("GOENV", config)
	t.Setenv("GOTOOLCHAIN", "local")
	for _, name := range []string{"GONOSUMDB", "GOPRIVATE", "GONOPROXY"} {
		t.Setenv(name, "*")
	}
	env := sourceInstallEnv("", filepath.Join(root, "modcache"))

	out, err := commandOutputEnv(t.Context(), 10*time.Second, 4<<10, env,
		"go", "env", "-json", "GOSUMDB", "GOPROXY", "GONOSUMDB", "GOPRIVATE", "GONOPROXY")

	require.NoError(t, err)
	var settings map[string]string
	require.NoError(t, json.Unmarshal(out, &settings))
	require.Equal(t, map[string]string{
		"GOSUMDB": "sum.golang.org", "GOPROXY": "direct",
		"GONOSUMDB": "none", "GOPRIVATE": "none", "GONOPROXY": "none",
	}, settings)
}

func TestGoUpdaterBuildsToTemporaryGOBINAndReplaces(t *testing.T) {
	for _, test := range []struct {
		name, tag string
		mise      bool
	}{
		{name: "Go-bin release", tag: "v1.2.3"},
		{name: "Go-bin head"},
		{name: "mise main", mise: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tag := test.tag
			version, query, originRef := tag, tag, "refs/tags/"+tag
			if tag == "" {
				version, query, originRef = "v1.2.4-0.20260906000000-aaaaaaaaaaaa", testCommit, ""
			}
			bin := t.TempDir()
			goPath := filepath.Join(bin, "go")
			script := fmt.Sprintf(`#!/bin/sh
[ "$GOPROXY" = direct ] && [ "$GOSUMDB" = sum.golang.org ] && [ -n "$GOMODCACHE" ] && [ "$GOCACHE" = "${GOMODCACHE%%/modcache}/gocache" ] && [ "$GOTMPDIR" = "${GOMODCACHE%%/modcache}/tmp" ] && [ "$GONOSUMDB" = none ] && [ "$GOPRIVATE" = none ] && [ "$GONOPROXY" = none ] || exit 7
case "$*" in *github.com/MuhametSmaili/hetki@%s) ;; *) exit 8 ;; esac
if [ "$1 $2" = "mod download" ]; then
  printf '{"Path":"github.com/MuhametSmaili/hetki","Version":"%s","Query":"%s","Sum":"h1:test","Origin":{"URL":"https://github.com/MuhametSmaili/hetki","Hash":"%s","Ref":"%s"}}\n'
  exit 0
fi
case "$*" in *GitCommit=%s*) ;; *) exit 9 ;; esac
case "$*" in *cmd.Version=%s*) ;; *) exit 10 ;; esac
mkdir -p "$GOBIN"
printf '#!/bin/sh\nprintf '\''hetki version %s\\ncommit: %s\\n'\''\n' > "$GOBIN/hetki"
chmod +x "$GOBIN/hetki"
`, query, version, query, testCommit, originRef, testCommit, version, version, testCommit)
			require.NoError(t, os.WriteFile(goPath, []byte(script), 0755))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			exePath := filepath.Join(bin, "hetki")
			if test.mise {
				var entry map[string]any
				exePath, entry = miseInstallation(t)
				stubMiseInstalls(t, entry)
			} else {
				fakeBinary(t, exePath, "v1.0.0")
				stubMise(t, "[]")
			}
			t.Setenv("GOBIN", filepath.Dir(exePath))
			updater, err := DetermineUpdater(t.Context(), exePath, Options{Head: tag == ""})
			require.NoError(t, err)

			require.NoError(t, updater.Update(t.Context(), testTarget(tag)))

			identity, err := installedIdentity(t.Context(), exePath)
			require.NoError(t, err)
			assert.Equal(t, Target{Tag: version, Commit: testCommit}, identity)
			assert.NoFileExists(t, exePath+".hetki-backup")
			workspaces, err := filepath.Glob(filepath.Join(filepath.Dir(exePath), ".hetki-source-*"))
			require.NoError(t, err)
			assert.Empty(t, workspaces)
		})
	}
}

func TestGoUpdaterBoundsInheritedOutputDescendant(t *testing.T) {
	t.Setenv("GH_TOKEN", "partial-secret")
	bin := t.TempDir()
	pidPath := filepath.Join(bin, "child.pid")
	goPath := filepath.Join(bin, "go")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1 $2" = "mod download" ]; then
  printf '{"Path":"github.com/MuhametSmaili/hetki","Version":"v1.2.3","Sum":"h1:test","Origin":{"URL":"https://github.com/MuhametSmaili/hetki","Hash":"%s","Ref":"refs/tags/v1.2.3"}}\n'
  exit 0
fi
mkdir -p "$GOBIN"
printf '#!/bin/sh\nprintf '\''hetki version v1.2.3\\ncommit: %s\\n'\''\n' > "$GOBIN/hetki"
chmod +x "$GOBIN/hetki"
mkfifo "$GOBIN/ready"
(printf 'partial-' >&2; printf 'ready\n' > "$GOBIN/ready"; sleep 10; printf 'secret\n' >&2) &
echo $! > %q
read -r ready < "$GOBIN/ready"
`, testCommit, testCommit, pidPath)
	require.NoError(t, os.WriteFile(goPath, []byte(script), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	exePath := filepath.Join(bin, "hetki")
	fakeBinary(t, exePath, "v1.0.0")
	started := time.Now()

	err := (&GoUpdater{exePath: exePath}).Update(context.Background(), testTarget("v1.2.3"))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "partial-")
	assert.Contains(t, err.Error(), "[truncated]")
	assert.Less(t, time.Since(started), 4*time.Second)
	pidBytes, readErr := os.ReadFile(pidPath)
	require.NoError(t, readErr)
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, parseErr)
	require.Eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }, time.Second, 10*time.Millisecond)
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exePath))
}

func TestServiceRunHead(t *testing.T) {
	for _, test := range []struct {
		name       string
		opts       Options
		commit     string
		resolveErr error
		wantErr    string
		updates    int
		dryRuns    int
	}{
		{name: "release to head", updates: 1},
		{name: "ignores semver ordering", opts: Options{CurrentVersion: "v99.0.0"}, updates: 1},
		{name: "unknown current commit", opts: Options{CurrentCommit: "unknown"}, updates: 1},
		{name: "new commit", opts: Options{CurrentCommit: strings.Repeat("b", 40)}, updates: 1},
		{name: "same commit", opts: Options{CurrentCommit: testCommit}},
		{name: "dry run", opts: Options{DryRun: true}, dryRuns: 1},
		{name: "pinned version conflict", opts: Options{TargetVersion: "v1.2.3"}, wantErr: "cannot be combined"},
		{name: "invalid commit", commit: "bad", wantErr: "valid commit"},
		{name: "resolution fails", resolveErr: errors.New("offline"), wantErr: "offline"},
		{name: "resolution canceled", resolveErr: context.Canceled, wantErr: "context canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			updater := &stubUpdater{}
			service := Service{
				SetVerbose:       func(bool) {},
				Executable:       func() (string, error) { return "/tmp/hetki", nil },
				DetermineUpdater: func(string) (Updater, error) { return updater, nil },
				ResolveTarget: func(context.Context, Options) (string, error) {
					t.Fatal("head must not resolve a release")
					return "", nil
				},
				ResolveHead: func(context.Context) (string, error) {
					if test.commit != "" {
						return test.commit, test.resolveErr
					}
					return testCommit, test.resolveErr
				},
			}
			opts := test.opts
			opts.Head = true
			err := service.Run(context.Background(), opts)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}
			if test.resolveErr != nil {
				require.ErrorIs(t, err, test.resolveErr)
			}
			assert.Equal(t, test.updates, updater.updateCalls)
			assert.Equal(t, test.dryRuns, updater.dryRunCalls)
			if test.updates+test.dryRuns > 0 {
				assert.Equal(t, Target{Commit: testCommit}, updater.lastTarget)
			}
		})
	}
}

func TestDetermineUpdaterReleaseAndHead(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	t.Setenv("GOBIN", filepath.Join(home, "go", "bin"))
	t.Setenv("GOPATH", filepath.Join(home, "go"))
	for _, dir := range []string{"go/bin", ".local/bin", "bin"} {
		exe := filepath.Join(home, dir, "hetki")
		require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0700))
		fakeBinary(t, exe, "v1.2.3")
		updater, err := DetermineUpdater(t.Context(), exe, Options{})
		require.NoError(t, err)
		if dir == "go/bin" {
			assert.IsType(t, &GoUpdater{}, updater)
		} else {
			assert.IsType(t, &BinaryUpdater{}, updater)
		}
		updater, err = DetermineUpdater(t.Context(), exe, Options{Head: true, Verbose: true})
		require.NoError(t, err)
		assert.Equal(t, &GoUpdater{exePath: exe, Verbose: true}, updater)
	}
}

func TestGoUpdaterRejectsUnverifiedSource(t *testing.T) {
	metadata := fmt.Sprintf(`{"Path":"github.com/MuhametSmaili/hetki","Version":"v1.2.3","Query":"%s","Sum":"h1:test","Origin":{"URL":"https://github.com/MuhametSmaili/hetki","Hash":"%s","Ref":"refs/tags/v1.2.3"}}`, testCommit, testCommit)
	for _, test := range []struct {
		name, tag, from, to string
	}{
		{name: "wrong module", from: `"Path":"github.com/MuhametSmaili/hetki"`, to: `"Path":"other"`},
		{name: "wrong repository", from: `"URL":"https://github.com/MuhametSmaili/hetki"`, to: `"URL":"other"`},
		{name: "wrong commit", from: `"Hash":"` + testCommit + `"`, to: `"Hash":"` + strings.Repeat("b", 40) + `"`},
		{name: "missing sum", from: `"Sum":"h1:test"`, to: `"Sum":""`},
		{name: "mutable query", from: `"Query":"` + testCommit + `"`, to: `"Query":"main"`},
		{name: "invalid version", from: `"Version":"v1.2.3"`, to: `"Version":"main"`},
		{name: "wrong release version", tag: "v1.2.3", from: `"Version":"v1.2.3"`, to: `"Version":"v1.2.4"`},
		{name: "wrong release ref", tag: "v1.2.3", from: `"Ref":"refs/tags/v1.2.3"`, to: `"Ref":"refs/heads/main"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			script := "#!/bin/sh\n[ \"$1 $2\" = \"mod download\" ] || exit 99\nprintf '%s\\n' '" + strings.Replace(metadata, test.from, test.to, 1) + "'\n"
			require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0755))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			exe := filepath.Join(bin, "hetki")
			fakeBinary(t, exe, "v1.0.0")
			err := (&GoUpdater{exePath: exe}).Update(context.Background(), testTarget(test.tag))
			require.ErrorContains(t, err, "source")
			assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
		})
	}
}

func TestGoUpdaterRequiresExactTagOrCommit(t *testing.T) {
	bin := t.TempDir()
	goPath := filepath.Join(bin, "go")
	require.NoError(t, os.WriteFile(goPath, []byte("#!/bin/sh\nexit 0\n"), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	require.ErrorContains(t, (&GoUpdater{}).Update(context.Background(), Target{}), "valid commit")
	require.ErrorContains(t, (&GoUpdater{}).Update(context.Background(), Target{Tag: "main", Commit: testCommit}), "release tag")
}
