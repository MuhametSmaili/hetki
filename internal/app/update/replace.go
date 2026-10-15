package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/MuhametSmaili/hetki/internal/logger"
)

// rejectSymlinkedInstall refuses to update through a symlink: replacing the
// link target implicitly is ambiguous, so the user updates the target path
// directly (D4: explicit handling, fail closed).
func rejectSymlinkedInstall(exePath string) error {
	info, err := os.Lstat(exePath)
	if err != nil {
		return fmt.Errorf("cannot inspect current installation: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; update the real binary path directly", exePath)
	}
	return nil
}

type updateLock struct {
	path string
	file *os.File
	info os.FileInfo
}

// replaceExecutable swaps in the verified binary and keeps a hard-link
// backup until the replacement reports the exact tag and commit.
func replaceExecutable(ctx context.Context, newPath, exePath string, target Target) (err error) {
	candidateInfo, err := inspectInstallCandidate(newPath)
	if err != nil {
		return err
	}
	lock, err := acquireUpdateLock(exePath)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
	}()

	oldInfo, candidateInfo, err := prepareReplacement(newPath, exePath, candidateInfo)
	if err != nil {
		return err
	}
	backupPath := exePath + ".hetki-backup"
	backupInfo, err := createRecoveryBackup(exePath, backupPath, oldInfo)
	if err != nil {
		return err
	}
	replaced := false
	defer func() {
		err = finishFailedReplacement(err, replaced, backupPath, backupInfo, exePath, candidateInfo)
	}()

	if err := replaceCandidate(ctx, newPath, candidateInfo, exePath, oldInfo, backupPath, backupInfo); err != nil {
		return err
	}
	replaced = true
	return verifyReplacement(ctx, exePath, candidateInfo, backupPath, backupInfo, target)
}

func acquireUpdateLock(exePath string) (*updateLock, error) {
	path := exePath + ".hetki-update-lock"
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("another update is active or left a lock at %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, errors.Join(err, os.Remove(path))
	}
	return &updateLock{path: path, file: file, info: info}, nil
}

func (l *updateLock) release() error {
	var err error
	if closeErr := l.file.Close(); closeErr != nil {
		err = closeErr
	}
	if removeErr := removeOwnedPath(l.path, l.info); removeErr != nil {
		err = errors.Join(err, fmt.Errorf("removing update lock: %w", removeErr))
	}
	return err
}

func prepareReplacement(newPath, exePath string, candidateInfo os.FileInfo) (os.FileInfo, os.FileInfo, error) {
	oldInfo, err := os.Lstat(exePath)
	if err != nil {
		return nil, nil, fmt.Errorf("inspecting current installation: %w", err)
	}
	if !oldInfo.Mode().IsRegular() {
		return nil, nil, errors.New("current installation is not a regular file")
	}
	mode := oldInfo.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	stat, ok := oldInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, nil, errors.New("cannot inspect current installation ownership")
	}
	if err := os.Chown(newPath, int(stat.Uid), int(stat.Gid)); err != nil {
		return nil, nil, fmt.Errorf("failed to preserve ownership: %w", err)
	}
	if err := os.Chmod(newPath, mode); err != nil {
		return nil, nil, fmt.Errorf("failed to set permissions: %w", err)
	}
	updatedCandidate, err := os.Lstat(newPath)
	if err != nil {
		return nil, nil, fmt.Errorf("inspecting install candidate after chmod: %w", err)
	}
	if !updatedCandidate.Mode().IsRegular() || !os.SameFile(candidateInfo, updatedCandidate) {
		return nil, nil, errors.New("install candidate identity changed during chmod")
	}
	return oldInfo, updatedCandidate, nil
}

func createRecoveryBackup(exePath, backupPath string, oldInfo os.FileInfo) (os.FileInfo, error) {
	if err := os.Link(exePath, backupPath); err != nil {
		return nil, fmt.Errorf("failed to create exclusive backup (remove %s only after recovery): %w", backupPath, err)
	}
	backupInfo, err := os.Lstat(backupPath)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("inspecting recovery backup: %w", err), os.Remove(backupPath))
	}
	if !os.SameFile(oldInfo, backupInfo) {
		_ = removeOwnedPath(backupPath, backupInfo)
		return nil, errors.New("installation changed while creating recovery backup")
	}
	return backupInfo, nil
}

func finishFailedReplacement(err error, replaced bool, backupPath string, backupInfo os.FileInfo, exePath string, candidateInfo os.FileInfo) error {
	if err == nil {
		return nil
	}
	if !replaced {
		if removeErr := removeOwnedPath(backupPath, backupInfo); removeErr != nil {
			return errors.Join(err, fmt.Errorf("removing unused recovery backup: %w", removeErr))
		}
		return err
	}
	if restoreErr := restoreOwnedBackup(backupPath, backupInfo, exePath, candidateInfo); restoreErr != nil {
		return errors.Join(err, fmt.Errorf("ROLLBACK FAILED; previous binary remains at %s: %w", backupPath, restoreErr))
	}
	return err
}

func replaceCandidate(ctx context.Context, newPath string, candidateInfo os.FileInfo, exePath string, oldInfo os.FileInfo, backupPath string, backupInfo os.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requirePathIdentity(exePath, oldInfo); err != nil {
		return fmt.Errorf("installation changed before replacement: %w", err)
	}
	if err := requirePathIdentity(newPath, candidateInfo); err != nil {
		return fmt.Errorf("candidate changed before replacement: %w", err)
	}
	if err := requirePathIdentity(backupPath, backupInfo); err != nil {
		return fmt.Errorf("recovery backup changed before replacement: %w", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}
	return nil
}

func verifyReplacement(ctx context.Context, exePath string, candidateInfo os.FileInfo, backupPath string, backupInfo os.FileInfo, target Target) error {
	identity, err := installedIdentity(ctx, exePath)
	if err != nil {
		return fmt.Errorf("verifying installed binary: %w", err)
	}
	if err := requirePathIdentity(exePath, candidateInfo); err != nil {
		return fmt.Errorf("installed binary changed during verification: %w", err)
	}
	if identity != target {
		return fmt.Errorf("installed binary reports %s at %s, expected %s at %s; rolling back",
			identity.Tag, identity.Commit, target.Tag, target.Commit)
	}
	if err := removeOwnedPath(backupPath, backupInfo); err != nil {
		return fmt.Errorf("removing recovery backup after verification: %w", err)
	}
	logger.Info("Installed binary verified at %s (%s)", identity.Tag, identity.Commit)
	return nil
}

func inspectInstallCandidate(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect install candidate: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("install candidate must be a regular, non-symlink file")
	}
	if info.Size() <= 0 || info.Size() > maxBinaryBytes {
		return nil, fmt.Errorf("install candidate size %d is outside the 1-%d byte bound", info.Size(), maxBinaryBytes)
	}
	return info, nil
}

func requirePathIdentity(path string, want os.FileInfo) error {
	got, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(got, want) {
		return errors.New("file identity does not match")
	}
	return nil
}

func removeOwnedPath(path string, want os.FileInfo) error {
	if err := requirePathIdentity(path, want); err != nil {
		return err
	}
	return os.Remove(path)
}

func restoreOwnedBackup(backupPath string, backupInfo os.FileInfo, exePath string, candidateInfo os.FileInfo) error {
	if err := requirePathIdentity(backupPath, backupInfo); err != nil {
		return fmt.Errorf("recovery backup identity changed: %w", err)
	}
	if err := requirePathIdentity(exePath, candidateInfo); err != nil {
		return fmt.Errorf("destination identity changed: %w", err)
	}
	return os.Rename(backupPath, exePath)
}

func installedIdentity(ctx context.Context, exePath string) (Target, error) {
	out, err := commandOutput(ctx, 30*time.Second, 4<<10, exePath, "--version")
	if err != nil {
		return Target{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return Target{}, fmt.Errorf("unexpected --version output %q", out)
	}
	version, versionOK := strings.CutPrefix(strings.TrimSpace(lines[0]), "hetki version ")
	commit, commitOK := strings.CutPrefix(strings.TrimSpace(lines[1]), "commit: ")
	if !versionOK || !commitOK {
		return Target{}, fmt.Errorf("unexpected --version output %q", out)
	}
	return Target{Tag: version, Commit: commit}, nil
}

func installedVersion(exePath string) (string, error) {
	identity, err := installedIdentity(context.Background(), exePath)
	return identity.Tag, err
}
