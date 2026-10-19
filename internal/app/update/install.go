package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/logger"
)

// DetermineUpdater checks installation ownership without modifying the executable.
func DetermineUpdater(ctx context.Context, exePath string, opts Options) (Updater, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A managed installation can also fall under GOBIN; check ownership first.
	updater, err := miseUpdater(ctx, exePath, opts)
	if err != nil {
		return nil, fmt.Errorf("checking mise installation: %w", summarizeFailure(err))
	}
	if updater != nil {
		return updater, nil
	}

	if installedViaGo(exePath) {
		return &GoUpdater{exePath: exePath, Verbose: opts.Verbose}, nil
	}
	if isUserLocalInstall(exePath) {
		if opts.Head {
			return &GoUpdater{exePath: exePath, Verbose: opts.Verbose}, nil
		}
		return &BinaryUpdater{exePath: exePath}, nil
	}
	return nil, errors.New(
		"unsupported installation: run the installed hetki, not `go run`; supported locations are Go-bin, ~/.local/bin, ~/bin, or mise's active Go-backend main installation with --head",
	)
}

func isUserLocalInstall(exePath string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return false
	}
	dir := filepath.Dir(resolved)
	return dir == filepath.Join(home, ".local", "bin") || dir == filepath.Join(home, "bin")
}

func installedViaGo(exePath string) bool {
	exeReal, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		logger.Debug("Failed to resolve symlinks for %s: %v", exePath, err)
		return false
	}
	logger.Debug("Resolved executable path: %s", exeReal)

	for _, dir := range goBinDirs() {
		dirReal, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		logger.Debug("Checking Go bin directory: %s", dirReal)
		if isWithinDir(exeReal, dirReal) {
			logger.Debug("Executable is within Go bin directory")
			return true
		}
	}
	return false
}

func goBinDirs() []string {
	var dirs []string
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, p := range filepath.SplitList(gopath) {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	}
	if len(dirs) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, "go", "bin"))
		}
	}
	return dirs
}

func isWithinDir(file, dir string) bool {
	rel, err := filepath.Rel(dir, file)
	return err == nil && !strings.HasPrefix(rel, "..")
}
