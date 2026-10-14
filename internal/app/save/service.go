package save

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	appshared "github.com/MSmaili/hetki/internal/app"
	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/manifest"
)

type Options struct {
	Path  string
	Name  string
	Local bool
	All   bool
}

type Service struct {
	DetectBackend func(...string) (backend.Backend, error)
}

func NewService(detectBackend func(...string) (backend.Backend, error)) Service {
	return Service{DetectBackend: detectBackend}
}

func (s Service) Run(ctx context.Context, opts Options) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateOptions(opts); err != nil {
		return "", err
	}

	b, err := s.detectBackend()
	if err != nil {
		return "", fmt.Errorf("failed to detect backend: %w\nHint: Make sure a supported multiplexer is running", err)
	}

	sessions, err := getTargetSessions(ctx, b, opts.All)
	if err != nil {
		return "", err
	}

	outputPath, err := determineSavePath(opts)
	if err != nil {
		return "", err
	}

	workspace, err := appshared.WorkspaceFromSessions(sessions, b.LayoutPreset)
	if err != nil {
		return "", fmt.Errorf("convert observed workspace: %w", err)
	}

	return saveWorkspace(ctx, workspace, outputPath, opts.All)
}

func (s Service) detectBackend() (backend.Backend, error) {
	if s.DetectBackend != nil {
		return s.DetectBackend()
	}
	return backend.Detect()
}

func validateOptions(opts Options) error {
	if opts.Path != "" && opts.Name != "" {
		return fmt.Errorf("cannot use both -p and -n flags\nUse either: hetki save -p <path> OR hetki save -n <name>")
	}
	return nil
}

func getTargetSessions(ctx context.Context, b backend.Backend, saveAll bool) ([]backend.Session, error) {
	result, err := b.QueryState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}

	if len(result.Sessions) == 0 {
		return nil, fmt.Errorf("no sessions found\nHint: Create a session first")
	}

	if saveAll {
		return result.Sessions, nil
	}

	return findCurrentSession(result)
}

func findCurrentSession(result backend.StateResult) ([]backend.Session, error) {
	if result.Active.Session == "" {
		return nil, fmt.Errorf("not in a session\nHint: Run this command from inside a multiplexer session, or pass --all with -p, -n, or the '.' target")
	}

	for _, s := range result.Sessions {
		if s.Name == result.Active.Session {
			return []backend.Session{s}, nil
		}
	}

	return nil, fmt.Errorf("session %q not found", result.Active.Session)
}

func determineSavePath(opts Options) (string, error) {
	if opts.Path != "" {
		return opts.Path, nil
	}

	resolver := manifest.NewResolver()

	if opts.Name != "" {
		return resolver.NamedPath(opts.Name)
	}

	if opts.Local {
		return resolver.LocalPath()
	}

	if opts.All {
		return "", fmt.Errorf("--all requires a destination\nUse: hetki save --all -p <path>, hetki save --all -n <name>, or hetki save --all '.'")
	}

	return "", fmt.Errorf("no save target specified\nHint: Use -p <path>, -n <name>, or . to specify where to save")
}

type destinationSnapshot struct {
	exists bool
	info   os.FileInfo
	data   []byte
}

var (
	syncSaveFile = func(file *os.File) error { return file.Sync() }
	renameSave   = os.Rename
	removeSave   = os.Remove
	syncSaveDir  = func(path string) error {
		dir, err := os.Open(path)
		if err != nil {
			return err
		}
		return errors.Join(dir.Sync(), dir.Close())
	}
)

func saveWorkspace(ctx context.Context, workspace *manifest.Workspace, outputPath string, saveAll bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}

	observed, err := readDestination(absPath)
	if err != nil {
		return "", err
	}

	if observed.exists {
		existing, err := manifest.Parse(observed.data, absPath)
		if err != nil {
			return "", fmt.Errorf("existing destination is invalid: %w", err)
		}
		if !saveAll {
			workspace = appshared.MergeWorkspaces(existing, workspace)
		}
	}

	data, err := manifest.Marshal(workspace, absPath)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := replaceDestination(ctx, absPath, data, observed); err != nil {
		return "", fmt.Errorf("writing workspace: %w", err)
	}
	return absPath, nil
}

func readDestination(path string) (destinationSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return destinationSnapshot{}, nil
	}
	if err != nil {
		return destinationSnapshot{}, fmt.Errorf("inspect destination: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return destinationSnapshot{}, fmt.Errorf("destination must not be a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return destinationSnapshot{}, fmt.Errorf("destination is not a regular file: %s", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return destinationSnapshot{}, fmt.Errorf("open destination: %w", err)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || !sameDestinationInfo(info, openedInfo) {
		_ = file.Close()
		if statErr != nil {
			return destinationSnapshot{}, fmt.Errorf("inspect opened destination: %w", statErr)
		}
		return destinationSnapshot{}, fmt.Errorf("destination changed while opening: %s", path)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, manifest.MaxManifestBytes+1))
	if len(data) > manifest.MaxManifestBytes {
		_ = file.Close()
		return destinationSnapshot{}, fmt.Errorf("destination exceeds %d bytes", manifest.MaxManifestBytes)
	}
	readInfo, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(readErr, statErr, closeErr); err != nil {
		return destinationSnapshot{}, fmt.Errorf("read destination: %w", err)
	}
	if !sameDestinationInfo(openedInfo, readInfo) {
		return destinationSnapshot{}, fmt.Errorf("destination changed while reading: %s", path)
	}
	return destinationSnapshot{exists: true, info: readInfo, data: data}, nil
}

func replaceDestination(ctx context.Context, path string, data []byte, observed destinationSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	mode := os.FileMode(0644)
	if observed.exists {
		mode = observed.info.Mode().Perm()
	}
	tempPath, err := writeSaveTemp(parent, filepath.Base(path), data, mode)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tempPath) }()

	if err := ensureDestinationUnchanged(path, observed); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := renameSave(tempPath, path); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	if err := syncSaveDir(parent); err != nil {
		if rollbackErr := restoreDestination(path, observed); rollbackErr != nil {
			return fmt.Errorf("sync destination directory: %w; uncertain state: rollback could not be proven: %v", err, rollbackErr)
		}
		return fmt.Errorf("sync destination directory: %w (replacement rolled back)", err)
	}
	return nil
}

func writeSaveTemp(parent, base string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(parent, "."+base+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if err := file.Chmod(mode); err != nil {
		cleanup()
		return "", fmt.Errorf("set temporary file mode: %w", err)
	}
	written, err := file.Write(data)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("write temporary file: %w", err)
	}
	if written != len(data) {
		cleanup()
		return "", fmt.Errorf("write temporary file: %w", io.ErrShortWrite)
	}
	if err := syncSaveFile(file); err != nil {
		cleanup()
		return "", fmt.Errorf("sync temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temporary file: %w", err)
	}
	return path, nil
}

func ensureDestinationUnchanged(path string, observed destinationSnapshot) error {
	current, err := readDestination(path)
	if err != nil {
		return err
	}
	if current.exists != observed.exists {
		return fmt.Errorf("destination changed since it was read; retry the save: %s", path)
	}
	if !observed.exists {
		return nil
	}
	if !sameDestinationInfo(observed.info, current.info) || !bytes.Equal(observed.data, current.data) {
		return fmt.Errorf("destination changed since it was read; retry the save: %s", path)
	}
	return nil
}

func sameDestinationInfo(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func restoreDestination(path string, observed destinationSnapshot) error {
	parent := filepath.Dir(path)
	if !observed.exists {
		if err := removeSave(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := syncSaveDir(parent); err != nil {
			return err
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("new destination still exists after rollback")
		}
		return nil
	}

	tempPath, err := writeSaveTemp(parent, filepath.Base(path)+".rollback", observed.data, observed.info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tempPath) }()
	if err := renameSave(tempPath, path); err != nil {
		return err
	}
	if err := syncSaveDir(parent); err != nil {
		return err
	}
	restored, err := readDestination(path)
	if err != nil {
		return err
	}
	if !restored.exists || !bytes.Equal(restored.data, observed.data) || restored.info.Mode().Perm() != observed.info.Mode().Perm() {
		return fmt.Errorf("restored destination does not match previous state")
	}
	return nil
}
