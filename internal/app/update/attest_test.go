package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	var raw map[string]any
	require.NoError(t, json.Unmarshal(bundles[1], &raw))
	assert.Equal(t, float64(2), raw["x"])
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
func stubGh(t *testing.T, version string, verifyJSON string) {
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
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
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
	stubGh(t, "2.97.0", subjectJSON("hetki-testos-testarch", digestHex))
	bundle := filepath.Join(dir, "bundle.json")
	require.NoError(t, os.WriteFile(bundle, []byte(`{}`), 0644))

	require.NoError(t, verifyAttestation(context.Background(), artifact, bundle, "hetki-testos-testarch", digestHex, testTarget("v1.0.0")))
}

func TestVerifyAttestationGhFailureFailsClosed(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "bin")
	require.NoError(t, os.WriteFile(artifact, []byte("x"), 0644))
	bundle := filepath.Join(dir, "bundle.json")
	require.NoError(t, os.WriteFile(bundle, []byte(`{}`), 0644))
	stubGh(t, "2.97.0", "")

	err := verifyAttestation(context.Background(), artifact, bundle, "hetki-testos-testarch", "d", testTarget("v1.0.0"))
	require.ErrorContains(t, err, "attestation verification failed")
}

func TestCheckVerificationResultMatrix(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		path := filepath.Join(dir, fmt.Sprintf("r%d", len(content)))
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
		return path
	}

	require.NoError(t, checkVerificationResult(write(subjectJSON("bin", "aaa")), "bin", "aaa"))
	require.ErrorContains(t, checkVerificationResult(write(subjectJSON("other", "aaa")), "bin", "aaa"), "no verified attestation")
	require.ErrorContains(t, checkVerificationResult(write(subjectJSON("bin", "bbb")), "bin", "aaa"), "no verified attestation")
	require.ErrorContains(t, checkVerificationResult(write(`[]`), "bin", "aaa"), "no verified attestation")
	require.ErrorContains(t, checkVerificationResult(write(`{`), "bin", "aaa"), "parsing gh attestation output")
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
