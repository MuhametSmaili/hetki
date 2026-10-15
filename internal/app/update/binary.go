package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/MuhametSmaili/hetki/internal/logger"
)

const (
	maxBinaryBytes    = 128 << 20
	maxChecksumsBytes = 64 << 10
)

// githubReleaseURL points at release downloads; a var so tests can redirect
// it to a stub server.
var githubReleaseURL = "https://github.com/"

// supportedPlatform mirrors the release artifact matrix.
func supportedPlatform(goos, goarch string) bool {
	switch {
	case goos == "darwin" && (goarch == "amd64" || goarch == "arm64"):
		return true
	case goos == "linux" && (goarch == "amd64" || goarch == "arm64"):
		return true
	default:
		return false
	}
}

type BinaryUpdater struct {
	exePath string
}

func (b *BinaryUpdater) Name() string { return "binary release" }

func (b *BinaryUpdater) DryRun(target Target) {
	binaryName := fmt.Sprintf("hetki-%s-%s", runtime.GOOS, runtime.GOARCH)
	logger.Info("Would download: %s%s/releases/download/%s/%s", githubReleaseURL, githubRepo, target.Tag, binaryName)
	logger.Info("Would verify its GitHub artifact attestation and replace: %s", b.exePath)
}

func (b *BinaryUpdater) Update(ctx context.Context, target Target) error {
	targetTag := target.Tag
	if targetTag == "" {
		return errors.New("no exact release tag resolved")
	}
	if _, err := ParseVersion(targetTag); err != nil {
		return fmt.Errorf("resolved version is not a release tag: %w", err)
	}
	if !validCommit(target.Commit) {
		return errors.New("resolved target has no valid commit")
	}
	if err := rejectSymlinkedInstall(b.exePath); err != nil {
		return err
	}
	// supportedPlatform mirrors the release matrix; anything else fails
	// before a download is attempted.
	if !supportedPlatform(runtime.GOOS, runtime.GOARCH) {
		return fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	binaryName := fmt.Sprintf("hetki-%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL := fmt.Sprintf("%s%s/releases/download/%s/%s", githubReleaseURL, githubRepo, targetTag, binaryName)
	checksumsURL := fmt.Sprintf("%s%s/releases/download/%s/checksums.txt", githubReleaseURL, githubRepo, targetTag)
	logger.Info("Downloading hetki %s for %s/%s...", targetTag, runtime.GOOS, runtime.GOARCH)

	tempFile, err := os.CreateTemp(filepath.Dir(b.exePath), ".hetki-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if err := downloadToFile(ctx, downloadURL, tempFile, maxBinaryBytes); err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}

	info, err := os.Stat(tempPath)
	if err != nil {
		return fmt.Errorf("failed to stat downloaded file: %w", err)
	}
	if info.Size() < 1<<20 {
		return fmt.Errorf("downloaded file is too small (%d bytes), expected a Go binary (>1MB)", info.Size())
	}
	if err := verifyBinaryChecksum(ctx, checksumsURL, tempPath, binaryName); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	digest, err := fileDigest(tempPath)
	if err != nil {
		return err
	}
	if _, err := verifyReleaseArtifact(ctx, tempPath, binaryName, digest, target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return replaceExecutable(ctx, tempPath, b.exePath, target)
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func downloadToFile(ctx context.Context, url string, f *os.File, maxBytes int64) (err error) {
	defer func() { err = errors.Join(err, f.Close()) }()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := githubHTTPClient(60 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	written, err := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if written > maxBytes {
		return fmt.Errorf("download exceeds %d bytes from %s", maxBytes, url)
	}
	return nil
}

func verifyBinaryChecksum(ctx context.Context, checksumsURL, filePath, binaryName string) error {
	logger.Info("Verifying checksum...")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return err
	}
	resp, err := githubHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download checksums: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsBytes+1))
	if err != nil {
		return fmt.Errorf("failed to read checksums: %w", err)
	}
	if len(data) > maxChecksumsBytes {
		return fmt.Errorf("checksums.txt exceeds %d bytes", maxChecksumsBytes)
	}

	var expectedHash string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) == 2 && parts[1] == binaryName {
			expectedHash = parts[0]
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read checksums: %w", err)
	}
	if expectedHash == "" {
		return fmt.Errorf("binary %s not found in checksums.txt", binaryName)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx: ctx, reader: f}); err != nil {
		return fmt.Errorf("failed to compute hash: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	logger.Info("Checksum verified: %s", actualHash)
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	return n, err
}
