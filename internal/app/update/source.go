package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MuhametSmaili/hetki/internal/logger"
)

const maxSourceWorkspaceBytes = 1 << 30

type GoUpdater struct {
	exePath string
	Verbose bool
}

func (g *GoUpdater) Name() string { return "go install" }

func (g *GoUpdater) DryRun(target Target) {
	ref := target.Tag
	if ref == "" {
		ref = target.Commit
	}
	logger.Info("Would run: go install %s@%s and verify commit %s", modulePath, ref, target.Commit)
	logger.Info("Would replace the existing binary at %s with rollback on verification failure", g.exePath)
}

func (g *GoUpdater) Update(ctx context.Context, target Target) error {
	if err := g.validateUpdate(target); err != nil {
		return err
	}
	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tempDir, err := os.MkdirTemp(filepath.Dir(g.exePath), ".hetki-source-*")
	if err != nil {
		return fmt.Errorf("creating source build directory: %w", err)
	}
	defer removeSourceDir(tempDir)
	sourceCtx, cancelSource := context.WithCancelCause(buildCtx)
	monitorDone := watchDirectorySize(sourceCtx, tempDir, maxSourceWorkspaceBytes, cancelSource)
	defer func() {
		cancelSource(nil)
		<-monitorDone
	}()

	ref := target.Tag
	if ref == "" {
		ref = target.Commit
	}
	module := modulePath + "@" + ref
	modCache := filepath.Join(tempDir, "modcache")
	if err := os.MkdirAll(filepath.Join(tempDir, "tmp"), 0700); err != nil {
		return fmt.Errorf("creating source temporary directory: %w", err)
	}
	version, err := verifySourceOrigin(sourceCtx, module, target, modCache)
	if err != nil {
		return err
	}
	target.Tag = version
	ldflags := fmt.Sprintf("-X %s/cmd.Version=%s -X %s/cmd.GitCommit=%s", modulePath, version, modulePath, target.Commit)
	args := []string{"install", "-ldflags", ldflags}
	if g.Verbose {
		args = append(args, "-v")
	}
	args = append(args, module)
	logger.Info("Installing from source: %s", module)
	logger.Debug("Running command: go %s", strings.Join(args, " "))

	cmdArgs := append([]string{"-c", `ulimit -f 131072; exec "$@"`, "_", "go"}, args...)
	cmd := exec.CommandContext(sourceCtx, "sh", cmdArgs...)
	cmd.Env = sourceInstallEnv(tempDir, modCache)
	configureCommandLifecycle(cmd)
	stdout, stderr := &boundedBuffer{max: maxProcessOutputBytes}, &boundedBuffer{max: maxProcessOutputBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	defer killCommandGroup(cmd)
	if err := cmd.Run(); err != nil {
		if cause := context.Cause(sourceCtx); cause != nil {
			return errors.Join(cause, err)
		}
		return fmt.Errorf("building source: %w", commandFailure(err, stderr))
	}
	if cause := context.Cause(sourceCtx); cause != nil {
		return cause
	}
	if exceeded, err := directorySizeExceeds(tempDir, maxSourceWorkspaceBytes); err != nil {
		return fmt.Errorf("checking source workspace size: %w", err)
	} else if exceeded {
		return fmt.Errorf("source workspace exceeds %d bytes", maxSourceWorkspaceBytes)
	}
	return replaceExecutable(ctx, filepath.Join(tempDir, "hetki"), g.exePath, target)
}

func (g *GoUpdater) validateUpdate(target Target) error {
	if target.Tag != "" {
		if _, err := ParseVersion(target.Tag); err != nil {
			return fmt.Errorf("resolved version is not a release tag: %w", err)
		}
	}
	if !validCommit(target.Commit) {
		return errors.New("resolved target has no valid commit")
	}
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("go binary not found in PATH")
	}
	return rejectSymlinkedInstall(g.exePath)
}

func watchDirectorySize(ctx context.Context, path string, maxBytes int64, cancel context.CancelCauseFunc) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				exceeded, err := directorySizeExceeds(path, maxBytes)
				if err != nil {
					cancel(fmt.Errorf("checking source workspace size: %w", err))
					return
				}
				if exceeded {
					cancel(fmt.Errorf("source workspace exceeds %d bytes", maxBytes))
					return
				}
			}
		}
	}()
	return done
}

func directorySizeExceeds(root string, maxBytes int64) (bool, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		size, err := regularFileSize(entry)
		if err != nil {
			return err
		}
		total += size
		if total > maxBytes {
			return fs.SkipAll
		}
		return nil
	})
	return total > maxBytes, err
}

func regularFileSize(entry fs.DirEntry) (int64, error) {
	if !entry.Type().IsRegular() {
		return 0, nil
	}
	info, err := entry.Info()
	if errors.Is(err, os.ErrNotExist) { // Go removes transient cache locks while we walk.
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func removeSourceDir(path string) {
	_ = filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = os.Chmod(path, info.Mode().Perm()|0700)
		}
		return nil
	})
	if err := os.RemoveAll(path); err != nil {
		logger.Warning("Could not remove source build directory %s: %v", path, err)
	}
}

func verifySourceOrigin(ctx context.Context, module string, target Target, modCache string) (string, error) {
	out, err := commandOutputEnv(ctx, 60*time.Second, 1<<20, sourceInstallEnv("", modCache), "go", "mod", "download", "-json", module)
	if err != nil {
		var failure struct{ Error string }
		if json.Unmarshal(out, &failure) == nil && failure.Error != "" {
			err = fmt.Errorf("%w: %s", err, processDiagnostic([]byte(failure.Error), false))
		}
		return "", fmt.Errorf("verifying source origin: %w", summarizeFailure(err))
	}
	var download struct {
		Path, Version, Query, Sum string
		Origin                    struct{ URL, Hash, Ref string }
	}
	if err := json.Unmarshal(out, &download); err != nil {
		return "", fmt.Errorf("decoding source origin: %w", err)
	}
	matchesTarget := download.Query == target.Commit
	if target.Tag != "" {
		matchesTarget = download.Version == target.Tag && download.Origin.Ref == "refs/tags/"+target.Tag
	}
	if download.Path != modulePath || download.Sum == "" || download.Origin.URL != "https://github.com/MuhametSmaili/hetki" ||
		download.Origin.Hash != target.Commit || !matchesTarget {
		return "", fmt.Errorf("source origin does not match requested commit %s", target.Commit)
	}
	if _, err := ParseVersion(download.Version); err != nil {
		return "", fmt.Errorf("source version rejected: %w", summarizeFailure(err))
	}
	return download.Version, nil
}

func sourceInstallEnv(gobin, modCache string) []string {
	env := os.Environ()
	for _, name := range []string{"GOBIN", "GOMODCACHE", "GOCACHE", "GOTMPDIR", "GOPROXY", "GOSUMDB", "GONOSUMDB", "GOPRIVATE", "GONOPROXY"} {
		prefix := name + "="
		for i := len(env) - 1; i >= 0; i-- {
			if strings.HasPrefix(env[i], prefix) {
				env = append(env[:i], env[i+1:]...)
			}
		}
	}
	root := filepath.Dir(modCache)
	// Empty exclusions fall back to persisted GOENV settings instead of clearing them.
	return append(env, "GOBIN="+gobin, "GOMODCACHE="+modCache, "GOCACHE="+filepath.Join(root, "gocache"),
		"GOTMPDIR="+filepath.Join(root, "tmp"), "GOPROXY=direct", "GOSUMDB=sum.golang.org", "GONOSUMDB=none", "GOPRIVATE=none", "GONOPROXY=none")
}
