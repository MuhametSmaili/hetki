package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/MuhametSmaili/hetki/internal/logger"
	"github.com/klauspost/compress/snappy"
)

const (
	// signerWorkflow pins which workflow may produce trusted artifacts (D4).
	signerWorkflow = "MuhametSmaili/hetki/.github/workflows/release.yml"
	// ghMinMajor/ghMinMinor is the oldest gh CLI with working
	// `attestation verify --bundle` (offline, unauthenticated).
	ghMinMajor, ghMinMinor = 2, 97
	// maxBundleBytes bounds the attestation API response and bundle files.
	maxBundleBytes          = 4 << 20
	maxAttestationBundles   = 30
	maxAttestationTotalByte = 16 << 20
	maxProcessOutputBytes   = 1 << 20
	// unsafeSkipVerifyEnv is the single loud emergency bypass (D4).
	unsafeSkipVerifyEnv = "HETKI_UNSAFE_SKIP_VERIFY"
)

// ghVersionPattern matches e.g. "gh version 2.63.2 (2025-01-30)".
var ghVersionPattern = regexp.MustCompile(`gh version (\d+)\.(\d+)`)

// unsafeSkipVerify reports whether the user set the emergency bypass.
func unsafeSkipVerify() bool {
	return os.Getenv(unsafeSkipVerifyEnv) == "1"
}

// fetchAttestationBundle retrieves the Sigstore bundle for an artifact digest
// from the repository attestation API. This endpoint needs no GitHub login;
// the bundle's signature is what carries the trust, not its transport.
func fetchAttestationBundles(ctx context.Context, sha256Digest string) ([]json.RawMessage, error) {
	url := apiBase + "repos/" + githubRepoPath + "attestations/sha256:" + sha256Digest

	req, err := newGitHubAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}
	resp, err := githubHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching attestation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no attestation exists for this artifact (expected a release built by %s)", signerWorkflow)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("attestation API returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Attestations []struct {
			Bundle    *json.RawMessage `json:"bundle"`
			BundleURL string           `json:"bundle_url"`
		} `json:"attestations"`
	}
	if err := decodeBoundedJSON(resp.Body, maxBundleBytes, &payload); err != nil {
		return nil, fmt.Errorf("decoding attestation response: %w", err)
	}
	if len(payload.Attestations) > maxAttestationBundles {
		return nil, fmt.Errorf("attestation response exceeds %d bundles", maxAttestationBundles)
	}
	bundles := make([]json.RawMessage, 0, len(payload.Attestations))
	totalBytes := 0
	for _, attestation := range payload.Attestations {
		switch {
		case attestation.BundleURL != "":
			bundle, err := fetchExternalBundle(ctx, attestation.BundleURL)
			if err != nil {
				return nil, err
			}
			totalBytes += len(bundle)
			bundles = append(bundles, bundle)
		case attestation.Bundle != nil:
			totalBytes += len(*attestation.Bundle)
			bundles = append(bundles, *attestation.Bundle)
		}
		if totalBytes > maxAttestationTotalByte {
			return nil, fmt.Errorf("attestation bundles exceed %d bytes total", maxAttestationTotalByte)
		}
	}
	if len(bundles) == 0 {
		return nil, errors.New("attestation response contains no bundles")
	}
	return bundles, nil
}

var externalBundleClient = func() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 5 {
			return errors.New("unsafe attestation bundle redirect")
		}
		return nil
	}}
}

func fetchExternalBundle(ctx context.Context, url string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "https" {
		return nil, errors.New("attestation bundle URL must use HTTPS")
	}
	resp, err := externalBundleClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching external attestation bundle: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external attestation bundle returned HTTP %d", resp.StatusCode)
	}
	compressed, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading external attestation bundle: %w", err)
	}
	if len(compressed) > maxBundleBytes {
		return nil, errors.New("external attestation bundle exceeds size limit")
	}
	decodedLen, err := snappy.DecodedLen(compressed)
	if err != nil || decodedLen > maxBundleBytes {
		return nil, errors.New("invalid or oversized snappy attestation bundle")
	}
	bundle, err := snappy.Decode(nil, compressed)
	if err != nil {
		return nil, fmt.Errorf("decompressing attestation bundle: %w", err)
	}
	return bundle, nil
}

// ghExecutable returns a gh CLI new enough for offline attestation
// verification, or a descriptive fail-closed error.
func ghExecutable(ctx context.Context) (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("gh CLI not found; install gh ≥ %d.%d or set %s=1 to skip verification at your own risk",
			ghMinMajor, ghMinMinor, unsafeSkipVerifyEnv)
	}
	out, err := commandOutput(ctx, 10*time.Second, 4<<10, path, "--version")
	if err != nil {
		return "", fmt.Errorf("running gh --version: %w", err)
	}
	match := ghVersionPattern.FindSubmatch(out)
	if match == nil {
		return "", fmt.Errorf("cannot determine gh version: %s", processDiagnostic(out, false))
	}

	major, err := strconv.Atoi(string(match[1]))
	if err != nil {
		return "", fmt.Errorf("cannot determine gh version: %s", processDiagnostic(out, false))
	}

	minor, err := strconv.Atoi(string(match[2]))
	if err != nil {
		return "", fmt.Errorf("cannot determine gh version: %s", processDiagnostic(out, false))
	}

	if major < ghMinMajor || (major == ghMinMajor && minor < ghMinMinor) {
		return "", fmt.Errorf("gh %d.%d is too old for attestation verification; install gh ≥ %d.%d",
			major, minor, ghMinMajor, ghMinMinor)
	}
	return path, nil
}

// verifyAttestation performs offline keyless verification of the downloaded
// artifact against the fetched bundle (D4): pinned repository, pinned signer
// workflow, no self-hosted runners, and a matching subject name.
func verifyAttestation(ctx context.Context, gh, artifactPath, bundlePath, binaryName, sha256Digest string, target Target) error {
	args := []string{
		"attestation", "verify", artifactPath,
		"--repo", githubRepo,
		"--bundle", bundlePath,
		"--signer-workflow", signerWorkflow,
		"--source-ref", "refs/tags/" + target.Tag,
		"--source-digest", target.Commit,
		"--deny-self-hosted-runners",
		"--format", "json",
	}
	stdout, err := commandOutput(ctx, 30*time.Second, maxProcessOutputBytes, gh, args...)
	if err != nil {
		return fmt.Errorf("attestation verification failed: %w", err)
	}
	return checkVerificationResult(stdout, binaryName, sha256Digest)
}

type verifyResult struct {
	VerificationResult struct {
		Statement struct {
			Subject []struct {
				Name   string            `json:"name"`
				Digest map[string]string `json:"digest"`
			} `json:"subject"`
		} `json:"statement"`
	} `json:"verificationResult"`
}

// checkVerificationResult requires at least one verified attestation whose
// subject name and digest match the downloaded artifact.
func checkVerificationResult(data []byte, binaryName, sha256Digest string) error {
	var results []verifyResult
	if err := json.Unmarshal(data, &results); err != nil {
		return fmt.Errorf("parsing gh attestation output: %w", err)
	}
	for _, result := range results {
		for _, subject := range result.VerificationResult.Statement.Subject {
			if subject.Name == binaryName && subject.Digest["sha256"] == sha256Digest {
				return nil
			}
		}
	}
	return fmt.Errorf("no verified attestation names %s with digest %s", binaryName, sha256Digest)
}

// verifyReleaseArtifact is the full D4 gate for a downloaded release binary:
// fail closed unless an emergency bypass is set.
func verifyReleaseArtifact(ctx context.Context, artifactPath, binaryName, sha256Digest string, target Target) (skipped bool, err error) {
	if unsafeSkipVerify() {
		logger.Warning("%s=1: skipping attestation verification. Only use this knowingly.", unsafeSkipVerifyEnv)
		return true, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	bundles, err := fetchAttestationBundles(ctx, sha256Digest)
	if err != nil {
		return false, err
	}
	gh, err := ghExecutable(ctx)
	if err != nil {
		return false, err
	}

	var verifyErr error
	for _, bundle := range bundles {
		bundleFile, err := os.CreateTemp("", "hetki-bundle-*.json")
		if err != nil {
			return false, err
		}
		bundlePath := bundleFile.Name()
		_, writeErr := bundleFile.Write(bundle)
		err = errors.Join(writeErr, bundleFile.Close())
		if err == nil {
			err = verifyAttestation(ctx, gh, artifactPath, bundlePath, binaryName, sha256Digest, target)
		}
		os.Remove(bundlePath)
		if err == nil {
			logger.Info("Attestation verified against %s", signerWorkflow)
			return false, nil
		}
		verifyErr = errors.Join(verifyErr, err)
	}
	if verifyErr != nil {
		return false, fmt.Errorf("verifying release attestation: %w", summarizeFailure(verifyErr))
	}
	return false, errors.New("no attestation bundle verified")
}
