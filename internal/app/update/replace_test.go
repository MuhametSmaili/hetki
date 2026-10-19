package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func testTarget(tag string) Target { return Target{Tag: tag, Commit: testCommit} }

func fakeBinary(t *testing.T, path, version string) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf 'hetki version %s\\ncommit: %s\\n'\n", version, testCommit)
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
}

func readScriptVersion(t *testing.T, path string) string {
	t.Helper()
	identity, err := installedIdentity(t.Context(), path)
	require.NoError(t, err)
	return identity.Tag
}

func TestReplaceExecutableSuccess(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")
	require.NoError(t, os.Chmod(exe, os.ModeSticky|0775))
	newPath := filepath.Join(dir, "new")
	fakeBinary(t, newPath, "v1.1.0")

	require.NoError(t, replaceExecutable(context.Background(), newPath, exe, testTarget("v1.1.0")))

	assert.Equal(t, "v1.1.0", readScriptVersion(t, exe))
	info, err := os.Stat(exe)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSticky|os.FileMode(0775), info.Mode()&(os.ModeSticky|os.ModePerm))
	assert.NoFileExists(t, exe+".hetki-backup")
	assert.NoFileExists(t, exe+".hetki-update-lock")
	assert.NoFileExists(t, newPath)
}

func TestReplaceExecutableRollsBackOnVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")
	require.NoError(t, os.Chmod(exe, os.ModeSetgid|0775))
	newPath := filepath.Join(dir, "new")
	fakeBinary(t, newPath, "v9.9.9") // lies about its version

	err := replaceExecutable(context.Background(), newPath, exe, testTarget("v1.1.0"))
	require.ErrorContains(t, err, "expected v1.1.0")

	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe), "previous binary restored")
	info, statErr := os.Stat(exe)
	require.NoError(t, statErr)
	assert.Equal(t, os.ModeSetgid|os.FileMode(0775), info.Mode()&(os.ModeSetgid|os.ModePerm))
	assert.NoFileExists(t, exe+".hetki-backup")
}

func TestReplaceExecutableFiltersRejectedIdentityOutput(t *testing.T) {
	for _, test := range []struct{ name, output, prefix, suffix string }{
		{name: "malformed output", output: "identity-secret", prefix: "verifying installed binary: unexpected --version output: "},
		{name: "mismatched identity", output: "hetki version identity-secret\ncommit: wrong", prefix: "installed binary reports ", suffix: ", expected v1.1.0 at " + testCommit + "; rolling back"},
		{name: "oversized malformed identity", output: strings.Repeat("x", 2048), prefix: "verifying installed binary: unexpected --version output: "},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			exe, candidate := filepath.Join(dir, "hetki"), filepath.Join(dir, "new")
			fakeBinary(t, exe, "v1.0.0")
			t.Setenv("GH_TOKEN", "identity-secret")
			t.Setenv("HETKI_TEST_IDENTITY", test.output)
			script := "#!/bin/sh\nprintf '%s' \"$HETKI_TEST_IDENTITY\"\n"
			require.NoError(t, os.WriteFile(candidate, []byte(script), 0755))

			err := replaceExecutable(context.Background(), candidate, exe, testTarget("v1.1.0"))

			require.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), test.prefix))
			assert.True(t, strings.HasSuffix(err.Error(), test.suffix))
			assert.NotContains(t, err.Error(), "identity-secret")
			detail := strings.TrimSuffix(strings.TrimPrefix(err.Error(), test.prefix), test.suffix)
			assert.LessOrEqual(t, len(detail), 512)
			assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
			assert.NoFileExists(t, exe+".hetki-backup")
			assert.NoFileExists(t, exe+".hetki-update-lock")
		})
	}
}

func TestReplaceExecutableNewBinaryBrokenRollsBack(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")
	newPath := filepath.Join(dir, "new")
	require.NoError(t, os.WriteFile(newPath, []byte("not a binary"), 0755))

	err := replaceExecutable(context.Background(), newPath, exe, testTarget("v1.1.0"))
	require.Error(t, err)
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe), "previous binary restored")
}

func TestReplaceExecutableRefusesSymlinkCandidate(t *testing.T) {
	dir := t.TempDir()
	exe, real, candidate := filepath.Join(dir, "hetki"), filepath.Join(dir, "real"), filepath.Join(dir, "candidate")
	fakeBinary(t, exe, "v1.0.0")
	fakeBinary(t, real, "v1.1.0")
	require.NoError(t, os.Symlink(real, candidate))

	err := replaceExecutable(context.Background(), candidate, exe, testTarget("v1.1.0"))
	require.ErrorContains(t, err, "regular, non-symlink")
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
}

func TestReplaceExecutableCancellationBeforeRenameLeavesDestinationUntouched(t *testing.T) {
	dir := t.TempDir()
	exe, candidate := filepath.Join(dir, "hetki"), filepath.Join(dir, "candidate")
	fakeBinary(t, exe, "v1.0.0")
	fakeBinary(t, candidate, "v1.1.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := replaceExecutable(ctx, candidate, exe, testTarget("v1.1.0"))
	require.Equal(t, context.Canceled, err)
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
	assert.NoFileExists(t, exe+".hetki-backup")
}

func TestReplaceExecutableCancellationDuringVerificationRollsBack(t *testing.T) {
	dir := t.TempDir()
	exe, candidate := filepath.Join(dir, "hetki"), filepath.Join(dir, "candidate")
	marker, pidPath := filepath.Join(dir, "started"), filepath.Join(dir, "child.pid")
	fakeBinary(t, exe, "v1.0.0")
	script := fmt.Sprintf("#!/bin/sh\nsleep 30 &\necho $! > %q\ntouch %q\nwait\nprintf 'hetki version v1.1.0\\ncommit: %s\\n'\n", pidPath, marker, testCommit)
	require.NoError(t, os.WriteFile(candidate, []byte(script), 0755))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- replaceExecutable(ctx, candidate, exe, testTarget("v1.1.0")) }()
	require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not stop after cancellation")
	}
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
	assert.NoFileExists(t, exe+".hetki-backup")
	assert.NoFileExists(t, exe+".hetki-update-lock")
	pidBytes, err := os.ReadFile(pidPath)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }, time.Second, 10*time.Millisecond)
}

func TestReplaceExecutableRefusesConcurrentUpdateLock(t *testing.T) {
	dir := t.TempDir()
	exe, candidate := filepath.Join(dir, "hetki"), filepath.Join(dir, "candidate")
	fakeBinary(t, exe, "v1.0.0")
	fakeBinary(t, candidate, "v1.1.0")
	require.NoError(t, os.WriteFile(exe+".hetki-update-lock", []byte("held"), 0600))

	err := replaceExecutable(context.Background(), candidate, exe, testTarget("v1.1.0"))
	require.ErrorContains(t, err, "another update is active")
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
}

func TestReplaceExecutableRefusesBackupSymlink(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")
	newPath := filepath.Join(dir, "new")
	fakeBinary(t, newPath, "v1.1.0")
	victim := filepath.Join(dir, "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0600))
	require.NoError(t, os.Symlink(victim, exe+".hetki-backup"))

	err := replaceExecutable(context.Background(), newPath, exe, testTarget("v1.1.0"))
	require.ErrorContains(t, err, "exclusive backup")
	assert.Equal(t, "keep", string(mustReadFile(t, victim)))
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestRejectSymlinkedInstall(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	fakeBinary(t, real, "v1.0.0")
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(real, link))

	require.ErrorContains(t, rejectSymlinkedInstall(link), "symlink")
	require.NoError(t, rejectSymlinkedInstall(real))
}

func TestInstalledIdentityParsesVersionAndCommit(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	require.NoError(t, os.WriteFile(exe, []byte("#!/bin/sh\nprintf 'hetki version v3.1.4\\ncommit: abc\\n'\n"), 0755))

	identity, err := installedIdentity(t.Context(), exe)

	require.NoError(t, err)
	assert.Equal(t, Target{Tag: "v3.1.4", Commit: "abc"}, identity)
}

func TestInstalledIdentityRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	require.NoError(t, os.WriteFile(exe, []byte("#!/bin/sh\necho garbage\n"), 0755))

	_, err := installedIdentity(t.Context(), exe)
	require.ErrorContains(t, err, "unexpected --version output")
}

func TestDownloadToFileRejectsOversize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 100))
	}))
	defer server.Close()
	file, err := os.CreateTemp(t.TempDir(), "dl")
	require.NoError(t, err)

	err = downloadToFile(context.Background(), server.URL, file, 10)
	require.ErrorContains(t, err, "exceeds")
}

func TestDownloadToFileAcceptsExactLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 10))
	}))
	defer server.Close()
	file, err := os.CreateTemp(t.TempDir(), "dl")
	require.NoError(t, err)

	require.NoError(t, downloadToFile(context.Background(), server.URL, file, 10))
}

// TestBinaryUpdaterUpdateFullFlow drives the whole verified path: bounded
// download, checksum, attestation, atomic replace, version check.
func TestBinaryUpdaterUpdateFullFlow(t *testing.T) {
	binaryName := fmt.Sprintf("hetki-%s-%s", runtime.GOOS, runtime.GOARCH)

	// The fake "binary" reports the target identity; pad it over the 1MiB floor.
	payload := []byte(fmt.Sprintf("#!/bin/sh\nprintf 'hetki version v1.0.1\\ncommit: %s\\n'\n#%s\n", testCommit, string(make([]byte, 1<<20))))
	digest := sha256.Sum256(payload)
	digestHex := hex.EncodeToString(digest[:])

	releaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/" + binaryName:
			w.Write(payload)
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", digestHex, binaryName)
		default:
			t.Errorf("unexpected download path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer releaseServer.Close()

	stubGh(t, "2.97.0", subjectJSON(binaryName, digestHex))
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"attestations":[{"bundle":%s}]}`, subjectJSON(binaryName, digestHex))
	})

	previous := githubReleaseURL
	githubReleaseURL = releaseServer.URL + "/"
	t.Cleanup(func() { githubReleaseURL = previous })

	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")

	updater := &BinaryUpdater{exePath: exe}
	require.NoError(t, updater.Update(context.Background(), testTarget("v1.0.1")))

	assert.Equal(t, "v1.0.1", readScriptVersion(t, exe))
	assert.NoFileExists(t, exe+".hetki-backup")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp/backup files left behind")
}

func TestBinaryUpdaterUpdateRefusesWithoutAttestation(t *testing.T) {
	binaryName := fmt.Sprintf("hetki-%s-%s", runtime.GOOS, runtime.GOARCH)
	payload := []byte(fmt.Sprintf("#!/bin/sh\nprintf 'hetki version v1.0.1\\n'\n#%s\n", string(make([]byte, 1<<20))))
	digest := sha256.Sum256(payload)
	digestHex := hex.EncodeToString(digest[:])

	releaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/" + binaryName:
			w.Write(payload)
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", digestHex, binaryName)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer releaseServer.Close()

	// A too-old gh makes verification unavailable; the update must fail
	// closed with the previous binary untouched.
	stubGh(t, "2.46.0", "")
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"attestations":[{"bundle":{}}]}`))
	})
	previous := githubReleaseURL
	githubReleaseURL = releaseServer.URL + "/"
	t.Cleanup(func() { githubReleaseURL = previous })

	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")

	err := (&BinaryUpdater{exePath: exe}).Update(context.Background(), testTarget("v1.0.1"))
	require.ErrorContains(t, err, "too old")
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe), "binary untouched")

	entries, _ := os.ReadDir(dir)
	assert.Len(t, entries, 1, "no temp files left behind")
}

func TestBinaryUpdaterUpdateRejectsMutableRefs(t *testing.T) {
	updater := &BinaryUpdater{exePath: filepath.Join(t.TempDir(), "hetki")}
	require.ErrorContains(t, updater.Update(context.Background(), Target{}), "exact release tag")
	require.ErrorContains(t, updater.Update(context.Background(), Target{Tag: "main", Commit: testCommit}), "release tag")
}

func TestSupportedPlatformMatrix(t *testing.T) {
	assert.True(t, supportedPlatform("darwin", "amd64"))
	assert.True(t, supportedPlatform("darwin", "arm64"))
	assert.True(t, supportedPlatform("linux", "amd64"))
	assert.True(t, supportedPlatform("linux", "arm64"))
	for _, p := range [][2]string{{"windows", "amd64"}, {"linux", "386"}, {"darwin", "riscv64"}} {
		assert.False(t, supportedPlatform(p[0], p[1]), "%s/%s", p[0], p[1])
	}
}

func TestBinaryUpdaterUpdateTamperedArtifactFails(t *testing.T) {
	binaryName := fmt.Sprintf("hetki-%s-%s", runtime.GOOS, runtime.GOARCH)
	// Truncated (under the 1MiB floor) and checksummed artifact.
	payload := []byte("tiny fake binary")
	digest := sha256.Sum256(payload)
	digestHex := hex.EncodeToString(digest[:])

	releaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/" + binaryName:
			w.Write(payload)
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", digestHex, binaryName)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer releaseServer.Close()
	previous := githubReleaseURL
	githubReleaseURL = releaseServer.URL + "/"
	t.Cleanup(func() { githubReleaseURL = previous })

	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")

	err := (&BinaryUpdater{exePath: exe}).Update(context.Background(), testTarget("v1.0.1"))
	require.ErrorContains(t, err, "too small")
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe), "binary untouched")
}

func TestBinaryUpdaterUpdateChecksumMismatchFails(t *testing.T) {
	binaryName := fmt.Sprintf("hetki-%s-%s", runtime.GOOS, runtime.GOARCH)
	payload := []byte(fmt.Sprintf("#!/bin/sh\nprintf 'hetki version v1.0.1\\n'\n#%s\n", string(make([]byte, 1<<20))))

	releaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/" + binaryName:
			w.Write(payload)
		case "/MuhametSmaili/hetki/releases/download/v1.0.1/checksums.txt":
			fmt.Fprintf(w, "%064x  %s\n", 0, binaryName) // wrong hash on purpose
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer releaseServer.Close()
	previous := githubReleaseURL
	githubReleaseURL = releaseServer.URL + "/"
	t.Cleanup(func() { githubReleaseURL = previous })

	dir := t.TempDir()
	exe := filepath.Join(dir, "hetki")
	fakeBinary(t, exe, "v1.0.0")

	err := (&BinaryUpdater{exePath: exe}).Update(context.Background(), testTarget("v1.0.1"))
	require.ErrorContains(t, err, "hash mismatch")
	assert.Equal(t, "v1.0.0", readScriptVersion(t, exe), "binary untouched")
}
