// Package directory resolves paths and completes directory names.
package directory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	maxEntries = 4096
	maxMatches = 8
)

// Suggestions contains directory paths that end with a separator.
// Incomplete is true if a limit can exclude matches.
type Suggestions struct {
	Paths      []string
	Incomplete bool
}

// Resolve expands ~/ and resolves relative paths from base without a shell.
func Resolve(value, base string) (string, error) {
	if value == "" {
		return "", errors.New("directory is required")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~"))
	} else if strings.HasPrefix(value, "~") {
		return "", errors.New("use ~/ for your home directory; ~user is not supported")
	}
	if !filepath.IsAbs(value) {
		if !filepath.IsAbs(base) {
			return "", errors.New("relative directory needs an absolute base path")
		}
		value = filepath.Join(base, value)
	}
	return filepath.Clean(value), nil
}

// Validate returns an absolute path for an existing directory.
func Validate(value, base string) (string, error) {
	path, err := Resolve(value, base)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("open directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is a file; choose a directory", path)
	}
	return path, nil
}

// Suggest reads up to 4096 entries in one directory and returns up to 8 matches.
// It checks for cancellation between file system calls.
// Cancellation cannot stop a blocked file system call.
func Suggest(ctx context.Context, value, base string) (Suggestions, error) {
	if err := ctx.Err(); err != nil {
		return Suggestions{}, err
	}
	if value == "~" {
		value = "~/"
	}
	parent, prefix := filepath.Split(value)
	path, err := Resolve(parentOrDot(parent), base)
	if err != nil {
		return Suggestions{}, err
	}
	folder, err := os.Open(path)
	if err != nil {
		return Suggestions{}, fmt.Errorf("list directory %q: %w", path, err)
	}
	defer folder.Close()

	return readSuggestions(ctx, folder, parent, prefix)
}

func readSuggestions(ctx context.Context, folder *os.File, parent, prefix string) (Suggestions, error) {
	result := Suggestions{}
	for scanned := 0; scanned < maxEntries; scanned += 64 {
		if err := ctx.Err(); err != nil {
			return Suggestions{}, err
		}
		entries, err := folder.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			return Suggestions{}, fmt.Errorf("read directory suggestions: %w", err)
		}
		if err := result.addMatches(ctx, folder.Name(), parent, prefix, entries); err != nil {
			return Suggestions{}, err
		}
		if errors.Is(err, io.EOF) {
			return result, nil
		}
	}
	result.Incomplete = true
	return result, nil
}

func (s *Suggestions) addMatches(ctx context.Context, folder, parent, prefix string, entries []os.DirEntry) error {
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}

		matches, err := matchesDirectory(folder, entry, prefix)
		if err != nil {
			return err
		}
		if matches {
			s.Paths = append(s.Paths, parent+entry.Name()+string(filepath.Separator))
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	slices.Sort(s.Paths)
	if len(s.Paths) > maxMatches {
		s.Incomplete = true
		s.Paths = s.Paths[:maxMatches]
	}
	return nil
}

func matchesDirectory(parent string, entry os.DirEntry, prefix string) (bool, error) {
	if !strings.HasPrefix(entry.Name(), prefix) || (strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(prefix, ".")) {
		return false, nil
	}
	if entry.IsDir() {
		return true, nil
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false, nil
	}

	path := filepath.Join(parent, entry.Name())
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect directory suggestion %q: %w", path, err)
	}
	return info.IsDir(), nil
}

func parentOrDot(parent string) string {
	if parent == "" {
		return "."
	}
	return parent
}
