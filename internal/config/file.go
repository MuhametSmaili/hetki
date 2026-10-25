package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// DefaultPath uses the same XDG location on Linux and macOS. Relative XDG roots
// are ignored; a missing or relative HOME cannot provide a fallback.
func DefaultPath() (string, error) {
	root := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(root) {
		home := os.Getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", fmt.Errorf("configuration path requires an absolute HOME or XDG_CONFIG_HOME")
		}
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "hetki", "config.yaml"), nil
}

// LoadFile reads an optional regular file, following symlinks for dotfiles.
// Context is checked between filesystem operations; it cannot interrupt a syscall.
func LoadFile(ctx context.Context, path string) (preferences Config, err error) {
	defer func() {
		if err != nil {
			preferences = Config{}
			err = fmt.Errorf("configuration %q: %w", path, err)
		}
	}()

	if err = ctx.Err(); err != nil {
		return Config{}, err
	}

	file, openErr := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if openErr != nil {
		if err = ctx.Err(); err != nil {
			return Config{}, err
		}
		if errors.Is(openErr, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, openErr
	}

	defer func() {
		closeErr := file.Close()
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close configuration: %w", closeErr))
		}
		err = errors.Join(err, ctx.Err())
	}()

	if err = ctx.Err(); err != nil {
		return Config{}, err
	}
	return readFile(ctx, file)
}

func readFile(ctx context.Context, file *os.File) (Config, error) {
	info, err := file.Stat()
	if err != nil {
		return Config{}, fmt.Errorf("inspect configuration: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return Config{}, err
	}
	if !info.Mode().IsRegular() {
		return Config{}, fmt.Errorf("configuration must be a regular file")
	}
	if info.Size() > maxBytes {
		return Config{}, fmt.Errorf("configuration exceeds 64 KiB")
	}

	preferences, err := Decode(file)
	if err != nil {
		return Config{}, err
	}
	if err = ctx.Err(); err != nil {
		return Config{}, err
	}
	return preferences, nil
}
