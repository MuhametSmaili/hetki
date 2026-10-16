package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const DefaultExt = ".yaml"

type Resolver struct {
	configDir func() (string, error)
}

func NewResolver() *Resolver {
	return &Resolver{
		configDir: GetConfigDir,
	}
}

// NamedPath returns a validated absolute, literal path in the workspace directory.
// Extensionless names prefer existing .yaml then .yml files, defaulting to .yaml.
// Missing files are allowed for saves; names and the config directory are not expanded.
func (r *Resolver) NamedPath(name string) (string, error) {
	filename, err := workspaceFilename(name)
	if err != nil {
		return "", err
	}

	configDir, err := r.configDir()
	if err != nil {
		return "", fmt.Errorf("getting config dir: %w", err)
	}
	path := filepath.Join(configDir, "workspaces", filename)
	if filepath.Ext(name) != "" {
		return resolvedFilePath(path)
	}

	for _, candidate := range []string{path, strings.TrimSuffix(path, DefaultExt) + ".yml"} {
		// Keep invalid or dangling entries selected so validation cannot silently
		// fall through to a different workspace.
		if _, err := os.Lstat(candidate); err == nil {
			return resolvedFilePath(candidate)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect workspace file %q: %w", candidate, err)
		}
	}
	return resolvedFilePath(path)
}

// FilePath resolves an explicit YAML file target, allowing a missing file for saves.
// Loading checks readability; saving retains its stricter symlink protections.
func FilePath(path string) (string, error) {
	// Check before joining tilde paths, which would erase directory-shaped suffixes.
	expanded := os.ExpandEnv(path)
	base := filepath.Base(expanded)
	if expanded == "" || base == "." || base == ".." || strings.HasSuffix(expanded, string(filepath.Separator)) {
		return "", fmt.Errorf("workspace path %q must identify a file, not a directory", path)
	}

	if strings.HasPrefix(expanded, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		expanded = home + string(filepath.Separator) + strings.TrimPrefix(expanded, "~")
	}
	return resolvedFilePath(expanded)
}

// resolvedFilePath validates a literal path without expanding its contents.
func resolvedFilePath(path string) (string, error) {
	absolute, err := absoluteFilePath(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path %q: %w", path, err)
	}

	info, err := os.Stat(absolute)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect workspace file %q: %w", absolute, err)
	}
	if err == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("workspace path %q is not a regular file", absolute)
	}
	if !hasValidExt(absolute) {
		return "", fmt.Errorf("workspace path %q must have a .yaml or .yml extension", absolute)
	}
	return absolute, nil
}

// Resolve through the last ".." physically before cleaning. The remaining suffix
// can be new directories; the final component stays intact for save's symlink check.
func absoluteFilePath(path string) (string, error) {
	directory, filename := filepath.Split(path)
	parts := strings.Split(directory, string(filepath.Separator))
	lastParent := -1
	for i, part := range parts {
		if part == ".." {
			lastParent = i
		}
	}
	if lastParent < 0 {
		return filepath.Abs(path)
	}

	prefix := strings.Join(parts[:lastParent+1], string(filepath.Separator))
	if !filepath.IsAbs(prefix) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		prefix = cwd + string(filepath.Separator) + prefix
	}
	parent, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		return "", err
	}
	suffix := strings.Join(parts[lastParent+1:], string(filepath.Separator))
	return filepath.Join(parent, suffix, filename), nil
}

func workspaceFilename(name string) (string, error) {
	if name == "" || name != strings.TrimSpace(name) || name == "." || name == ".." {
		return "", fmt.Errorf("invalid workspace name %q", name)
	}
	if strings.ContainsAny(name, `/\\`) || filepath.IsAbs(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("workspace name must be one filename: %q", name)
	}

	ext := filepath.Ext(name)
	if ext == "" {
		return name + DefaultExt, nil
	}
	if !hasValidExt(name) || strings.TrimSuffix(name, ext) == "" {
		return "", fmt.Errorf("invalid workspace extension %q (use .yaml or .yml)", ext)
	}
	return name, nil
}

func hasValidExt(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}
