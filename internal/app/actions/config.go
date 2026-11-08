package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/MuhametSmaili/hetki/internal/config"
	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 1024 * 1024

// DefaultPath keeps recipes separate from optional presentation preferences.
func DefaultPath() (string, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "actions.yaml"), nil
}

// Load reads a required regular file. Invalid recipes never fall back to defaults.
func Load(ctx context.Context, path string) (catalog Catalog, err error) {
	if err := ctx.Err(); err != nil {
		return Catalog{}, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Catalog{}, fmt.Errorf("open actions %q: %w", path, err)
	}
	defer func() { err = errors.Join(err, file.Close(), ctx.Err()) }()

	info, err := file.Stat()
	if err != nil {
		return Catalog{}, err
	}
	if !info.Mode().IsRegular() {
		return Catalog{}, fmt.Errorf("actions must be a regular file")
	}
	return Decode(file)
}

// Decode validates one bounded YAML document, including duplicate/unknown keys.
func Decode(reader io.Reader) (Catalog, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err != nil {
		return Catalog{}, err
	}
	if len(data) > maxConfigBytes {
		return Catalog{}, fmt.Errorf("actions exceed 1 MiB")
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return Catalog{}, fmt.Errorf("decode actions: %w", err)
	}
	if err := validateYAML(&node, 0); err != nil {
		return Catalog{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode actions: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Catalog{}, fmt.Errorf("actions require exactly one YAML document")
	}
	if err := catalog.validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func validateYAML(node *yaml.Node, depth int) error {
	if depth > 24 || node.Kind == yaml.AliasNode {
		return fmt.Errorf("actions cannot contain aliases or exceed YAML depth 24")
	}
	for _, child := range node.Content {
		if err := validateYAML(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
