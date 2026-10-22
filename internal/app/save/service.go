package save

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/manifest"
)

type Options struct {
	// Nil selectors are omitted; non-nil empty values are invalid.
	Path *string
	Name *string
	All  bool
}

// Backend provides the observation and layout export operations used by saving.
type Backend interface {
	QueryState(context.Context) (backend.StateResult, error)
	LayoutPreset(string) (string, error)
}

type Service struct {
	DetectBackend  func() (Backend, error)
	validateLayout func(string) error
}

// NewService binds backend detection and a required pure authored-layout policy.
func NewService(detectBackend func() (Backend, error), validateLayout func(string) error) Service {
	return Service{DetectBackend: detectBackend, validateLayout: validateLayout}
}

func (s Service) Run(ctx context.Context, opts Options) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	outputPath, err := appshared.ResolveWorkspaceTarget(opts.Name, opts.Path)
	if err != nil {
		return "", appshared.WithHint(err, "Use hetki save -n your_workspace or hetki save -p ./path/to/your_workspace.yaml. Replace the example name or path with your own.")
	}

	if s.validateLayout == nil {
		return "", manifest.ErrLayoutValidatorRequired
	}

	b, err := s.detectBackend()
	if err != nil {
		return "", appshared.WithHint(fmt.Errorf("failed to detect backend: %w", err), "Install tmux and make sure it is available on PATH.")
	}

	sessions, err := getTargetSessions(ctx, b, opts.All)
	if err != nil {
		return "", err
	}

	workspace, err := appshared.WorkspaceFromSessions(sessions, b.LayoutPreset)
	if err != nil {
		return "", fmt.Errorf("convert observed workspace: %w", err)
	}

	return saveWorkspace(ctx, workspace, outputPath, opts.All, s.validateLayout)
}

func (s Service) detectBackend() (Backend, error) {
	if s.DetectBackend != nil {
		return s.DetectBackend()
	}
	return backend.Detect()
}

func getTargetSessions(ctx context.Context, b Backend, saveAll bool) ([]backend.Session, error) {
	result, err := b.QueryState(ctx)
	if err != nil {
		return nil, appshared.WithHint(fmt.Errorf("failed to query sessions: %w", err), "Check access to the tmux server and retry.")
	}

	if len(result.Sessions) == 0 {
		return nil, appshared.WithHint(errors.New("no sessions found"), "Create a tmux session, then save again.")
	}

	if saveAll {
		return result.Sessions, nil
	}

	return findCurrentSession(result)
}

func findCurrentSession(result backend.StateResult) ([]backend.Session, error) {
	if result.Active.Session == "" {
		return nil, appshared.WithHint(errors.New("not in a session"), "Run save inside tmux, or pass --all with --name or --path.")
	}

	for _, s := range result.Sessions {
		if s.Name == result.Active.Session {
			return []backend.Session{s}, nil
		}
	}

	return nil, fmt.Errorf("session %q not found", result.Active.Session)
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

func saveWorkspace(ctx context.Context, workspace *manifest.Workspace, outputPath string, saveAll bool, validateLayout func(string) error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if validateLayout == nil {
		return "", manifest.ErrLayoutValidatorRequired
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
		existing, err := manifest.Parse(observed.data, absPath, validateLayout)
		if err != nil {
			return "", appshared.WithHint(fmt.Errorf("existing destination is invalid: %w", err), "Fix the existing YAML or use a new destination file.")
		}
		if err := preserveCommands(existing, workspace, validateLayout); err != nil {
			return "", err
		}
		if !saveAll {
			workspace = appshared.MergeWorkspaces(existing, workspace)
		}
	}

	data, err := manifest.Marshal(workspace, absPath, validateLayout)
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
			return fmt.Errorf("sync destination directory: %w; uncertain state: rollback could not be proven: %w", err, rollbackErr)
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
