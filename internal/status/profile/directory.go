package profile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

const (
	MaxProfiles = 64
	maxEntries  = 256
)

// Catalog is immutable after loading. Match returns a value copy; a missing
// command is not an explicit profile and leaves default selection to the caller.
type Catalog struct {
	byCommand map[string]Profile
}

func (c *Catalog) Match(command string) (Profile, bool) {
	if c == nil {
		return Profile{}, false
	}
	p, found := c.byCommand[command]
	return p, found
}

func DefaultDirectory() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("status configuration directory must be absolute")
	}
	return filepath.Join(dir, "hetki", "status.d"), nil
}

// LoadDirectory atomically loads regular YAML files. Missing directories yield an
// empty catalog; invalid files or duplicate IDs/commands reject the whole catalog.
// Configuration is a startup snapshot; changes require a daemon restart.
func LoadDirectory(ctx context.Context, dir string) (*Catalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := directoryEntries(dir)
	if err != nil {
		return nil, fmt.Errorf("read status profiles %s: %w", dir, err)
	}
	catalog := &Catalog{byCommand: make(map[string]Profile)}
	ids := make(map[string]bool)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		if len(catalog.byCommand) == MaxProfiles {
			return nil, fmt.Errorf("status profiles exceed %d files", MaxProfiles)
		}

		p, err := loadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("status profile %s: %w", entry.Name(), err)
		}
		if ids[p.ID] || catalog.byCommand[p.Command].ID != "" {
			return nil, fmt.Errorf("status profile %s: duplicate id or command", entry.Name())
		}
		ids[p.ID] = true
		catalog.byCommand[p.Command] = p
	}
	return catalog, nil
}

func directoryEntries(dir string) ([]os.DirEntry, error) {
	file, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, readErr := file.ReadDir(maxEntries + 1)
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, errors.Join(readErr, closeErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > maxEntries {
		return nil, fmt.Errorf("directory exceeds %d entries", maxEntries)
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func loadFile(path string) (Profile, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Profile{}, err
	}
	p, loadErr := loadRegular(file)
	if err := errors.Join(loadErr, file.Close()); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func loadRegular(file *os.File) (Profile, error) {
	info, err := file.Stat()
	if err != nil {
		return Profile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxProfileBytes {
		return Profile{}, fmt.Errorf("expected a regular YAML file up to %d bytes", MaxProfileBytes)
	}
	return Load(file)
}
