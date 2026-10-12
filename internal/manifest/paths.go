package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveSessionRoot(path, cwd string) (string, error) {
	directory, err := expandedDirectory(path, cwd)
	if err != nil {
		return "", err
	}

	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("resolve session root: %w", err)
	}

	info, err := os.Stat(directory)
	if err != nil {
		return "", fmt.Errorf("inspect session root %q: %w", directory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("session root %q is not a directory", directory)
	}
	return directory, nil
}

func resolveDirectory(path, base string) (string, error) {
	if path == "" {
		return base, nil
	}

	directory, err := expandedDirectory(path, base)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(directory)
	if err == nil {
		return resolved, nil
	}
	// Missing window/pane directories are allowed. Do not clean away missing
	// components and pretend their parent directory was the requested target.
	if errors.Is(err, os.ErrNotExist) {
		return directory, nil
	}
	return "", fmt.Errorf("resolve directory %q: %w", directory, err)
}

// expandedDirectory anchors explicit paths without cleaning components before
// symlink resolution: link/.. must use the physical parent of the link target.
func expandedDirectory(path, base string) (string, error) {
	path = os.ExpandEnv(path)
	if path == "" {
		return "", fmt.Errorf("path expands to an empty value; set a directory or omit the field")
	}
	if reason := checkPath(path); reason != "" {
		return "", fmt.Errorf("%s", reason)
	}

	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if reason := checkPath(home); reason != "" {
			return "", fmt.Errorf("home directory %s", reason)
		}
		path = joinDirectory(home, strings.TrimPrefix(path, "~"))
	}

	if !filepath.IsAbs(path) {
		path = joinDirectory(base, path)
	}
	if len(path) > MaxPathLength {
		return "", fmt.Errorf("resolved path is too long (max %d bytes)", MaxPathLength)
	}
	return path, nil
}

func joinDirectory(base, path string) string {
	separator := string(filepath.Separator)
	return strings.TrimRight(base, separator) + separator + strings.TrimLeft(path, separator)
}
