package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/klauspost/compress/snappy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchAttestationBundlesReturnsAllBundles(t *testing.T) {
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/MuhametSmaili/hetki/attestations/sha256:abc", r.URL.Path)
		w.Write([]byte(`{"attestations":[{"bundle":{"mediaType":"1","x":1}},{"bundle":{"x":2}}]}`))
	})

	bundles, err := fetchAttestationBundles(context.Background(), "abc")
	require.NoError(t, err)
	require.Len(t, bundles, 2)
	assert.JSONEq(t, `{"x":2}`, string(bundles[1]))
}

func TestFetchAttestationBundlesResolvesSnappyBundleURL(t *testing.T) {
	t.Setenv("GH_TOKEN", "api-token")
	t.Setenv("GITHUB_TOKEN", "")
	want := []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	bundleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"), "external bundles must stay anonymous")
		w.Write(snappy.Encode(nil, want))
	}))
	defer bundleServer.Close()
	previousClient := externalBundleClient
	externalBundleClient = bundleServer.Client
	t.Cleanup(func() { externalBundleClient = previousClient })
	stubUpdateHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "api.github.com", r.URL.Host)
		assert.Equal(t, "Bearer api-token", r.Header.Get("Authorization"))
		fmt.Fprintf(w, `{"attestations":[{"bundle":null,"bundle_url":%q}]}`, bundleServer.URL)
	})

	bundles, err := fetchAttestationBundles(context.Background(), "abc")
	require.NoError(t, err)
	require.Len(t, bundles, 1)
	assert.JSONEq(t, string(want), string(bundles[0]))
}

func TestExternalBundlePreservesReadErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "cancellation", err: context.Canceled},
		{name: "interrupted response", err: io.ErrUnexpectedEOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := externalBundleClient
			t.Cleanup(func() { externalBundleClient = previous })
			externalBundleClient = func() *http.Client {
				return &http.Client{Transport: updateRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(iotest.ErrReader(test.err))}, nil
				})}
			}

			bundle, err := fetchExternalBundle(t.Context(), "https://example.test/bundle")

			require.ErrorIs(t, err, test.err)
			require.Nil(t, bundle)
		})
	}
}

func TestFetchAttestationBundleNotFoundFailsClosed(t *testing.T) {
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := fetchAttestationBundles(context.Background(), "abc")
	require.ErrorContains(t, err, "no attestation exists")
}

func TestFetchAttestationBundleEmptyListFails(t *testing.T) {
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"attestations":[]}`))
	})

	_, err := fetchAttestationBundles(context.Background(), "abc")
	require.ErrorContains(t, err, "no bundles")
}

func TestFetchAttestationBundleMalformedFails(t *testing.T) {
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{not json`))
	})

	_, err := fetchAttestationBundles(context.Background(), "abc")
	require.ErrorContains(t, err, "decoding attestation response")
}

func TestFetchAttestationBundleServerErrFails(t *testing.T) {
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := fetchAttestationBundles(context.Background(), "abc")
	require.ErrorContains(t, err, "HTTP 403")
}

// stubGh writes a gh stub into PATH. verifyJSON is the JSON the stub prints
// for `attestation verify` (empty means: exit 1).
func stubGh(t *testing.T, version string, verifyJSON string) string {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n"
	if version == "" {
		script += "exit 9\n"
	} else {
		script += fmt.Sprintf("if [ \"$1\" = \"--version\" ]; then echo \"gh version %s (2025-01-01)\"; exit 0; fi\n", version)
		script += fmt.Sprintf(`args=" $* "
for required in "--repo MuhametSmaili/hetki" "--bundle " "--signer-workflow MuhametSmaili/hetki/.github/workflows/release.yml" "--source-digest %s" "--deny-self-hosted-runners" "--format json"; do
  case "$args" in *" $required"*) ;; *) echo "missing $required" >&2; exit 8 ;; esac
done
case "$args" in *" --source-ref refs/tags/v"*) ;; *) echo "missing source ref" >&2; exit 8 ;; esac
`, testCommit)
		if verifyJSON == "" {
			script += "echo 'no attestations' >&2\nexit 1\n"
		} else {
			script += fmt.Sprintf("cat <<'EOF'\n%s\nEOF\n", verifyJSON)
		}
	}
	path := filepath.Join(bin, "gh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

func TestGhExecutableGatesVersion(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := ghExecutable(context.Background())
	require.ErrorContains(t, err, "gh CLI not found")

	stubGh(t, "2.96.9", "")
	_, err = ghExecutable(context.Background())
	require.ErrorContains(t, err, "too old")

	stubGh(t, "2.97.0", "")
	gh, err := ghExecutable(context.Background())
	require.NoError(t, err)
	assert.Contains(t, gh, "gh")

	stubGh(t, "3.1.4", "")
	_, err = ghExecutable(context.Background())
	require.NoError(t, err)

	t.Setenv("GH_TOKEN", "version-token")
	stubGh(t, "invalid version-token", "")
	_, err = ghExecutable(context.Background())
	require.ErrorContains(t, err, "cannot determine gh version")
	assert.NotContains(t, err.Error(), "version-token")

	overflow := strings.Repeat("9", 40)
	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "overflowing major", version: overflow + ".0.0"},
		{name: "overflowing minor", version: "2." + overflow + ".0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GH_TOKEN", overflow)
			stubGh(t, tc.version, "")

			path, err := ghExecutable(t.Context())

			require.ErrorContains(t, err, "cannot determine gh version")
			assert.Empty(t, path)
			assert.NotContains(t, err.Error(), overflow)
		})
	}
}

func subjectJSON(name, digest string) string {
	return fmt.Sprintf(`[{"verificationResult":{"statement":{"subject":[{"name":%q,"digest":{"sha256":%q}}]}}}]`, name, digest)
}

func TestVerifyAttestationEndToEndPasses(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "hetki-testos-testarch")
	content := []byte("fake binary")
	require.NoError(t, os.WriteFile(artifact, content, 0644))
	digest := sha256.Sum256(content)
	digestHex := hex.EncodeToString(digest[:])
	gh := stubGh(t, "2.97.0", subjectJSON("hetki-testos-testarch", digestHex))
	bundle := filepath.Join(dir, "bundle.json")
	require.NoError(t, os.WriteFile(bundle, []byte(`{}`), 0644))

	require.NoError(t, verifyAttestation(t.Context(), gh, artifact, bundle, "hetki-testos-testarch", digestHex, testTarget("v1.0.0")))
}

func TestVerifyAttestationGhFailureFailsClosed(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "bin")
	require.NoError(t, os.WriteFile(artifact, []byte("x"), 0644))
	bundle := filepath.Join(dir, "bundle.json")
	require.NoError(t, os.WriteFile(bundle, []byte(`{}`), 0644))
	gh := stubGh(t, "2.97.0", "")

	err := verifyAttestation(t.Context(), gh, artifact, bundle, "hetki-testos-testarch", "d", testTarget("v1.0.0"))
	require.ErrorContains(t, err, "attestation verification failed")
	assert.Contains(t, err.Error(), "no attestations")
}

func TestCheckVerificationResultMatrix(t *testing.T) {
	require.NoError(t, checkVerificationResult([]byte(subjectJSON("bin", "aaa")), "bin", "aaa"))
	require.ErrorContains(t, checkVerificationResult([]byte(subjectJSON("other", "aaa")), "bin", "aaa"), "no verified attestation")
	require.ErrorContains(t, checkVerificationResult([]byte(subjectJSON("bin", "bbb")), "bin", "aaa"), "no verified attestation")
	require.ErrorContains(t, checkVerificationResult([]byte(`[]`), "bin", "aaa"), "no verified attestation")
	require.ErrorContains(t, checkVerificationResult([]byte(`{`), "bin", "aaa"), "parsing gh attestation output")
}

func TestVerifyReleaseArtifactBypass(t *testing.T) {
	t.Setenv(unsafeSkipVerifyEnv, "1")

	skipped, err := verifyReleaseArtifact(context.Background(), "/nonexistent", "bin", "d", testTarget("v1.0.0"))
	require.NoError(t, err)
	assert.True(t, skipped)
}

func TestVerifyReleaseArtifactFullFlow(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "hetki-testos-testarch")
	content := []byte("fake binary bytes")
	require.NoError(t, os.WriteFile(artifact, content, 0644))
	digest := sha256.Sum256(content)
	digestHex := hex.EncodeToString(digest[:])

	stubGh(t, "2.97.0", subjectJSON("hetki-testos-testarch", digestHex))
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"attestations":[{"bundle":%s}]}`, subjectJSON("hetki-testos-testarch", digestHex))
	})

	skipped, err := verifyReleaseArtifact(context.Background(), artifact, "hetki-testos-testarch", digestHex, testTarget("v1.0.0"))
	require.NoError(t, err)
	assert.False(t, skipped)
}

func TestVerifyReleaseArtifactBoundsFailureSummary(t *testing.T) {
	for _, test := range []struct {
		name, diagnostic string
		bundles          int
	}{
		{name: "one long explanation", diagnostic: strings.Repeat("detail ", 100) + "no attestations", bundles: 1},
		{name: "many explanations", diagnostic: "no attestations", bundles: maxAttestationBundles},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			artifact := filepath.Join(dir, "bin")
			require.NoError(t, os.WriteFile(artifact, []byte("x"), 0644))
			versionChecks := filepath.Join(dir, "version-checks")
			t.Setenv("HETKI_TEST_VERSION_CHECKS", versionChecks)
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo checked >> \"$HETKI_TEST_VERSION_CHECKS\"; echo 'gh version 2.97.0'; exit 0; fi\nprintf '%s' \"$HETKI_TEST_DIAGNOSTIC\" >&2\nexit 1\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("HETKI_TEST_DIAGNOSTIC", test.diagnostic)
			stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
				bundles := strings.TrimSuffix(strings.Repeat(`{"bundle":{}},`, test.bundles), ",")
				fmt.Fprintf(w, `{"attestations":[%s]}`, bundles)
			})

			_, err := verifyReleaseArtifact(context.Background(), artifact, "bin", "d", testTarget("v1.0.0"))

			require.ErrorContains(t, err, "no attestations")
			const prefix = "verifying release attestation: "
			assert.True(t, strings.HasPrefix(err.Error(), prefix))
			assert.Contains(t, err.Error(), "[truncated]")
			assert.LessOrEqual(t, len(strings.TrimPrefix(err.Error(), prefix)), 512)
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 1, exitErr.ExitCode())
			checks, readErr := os.ReadFile(versionChecks)
			require.NoError(t, readErr)
			assert.Equal(t, "checked\n", string(checks))
		})
	}
}

func TestVerifyReleaseArtifactNoBundleFailsClosed(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "bin")
	require.NoError(t, os.WriteFile(artifact, []byte("x"), 0644))
	stubGh(t, "2.97.0", "")
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := verifyReleaseArtifact(context.Background(), artifact, "bin", "d", testTarget("v1.0.0"))
	require.ErrorContains(t, err, "no attestation exists")
}
