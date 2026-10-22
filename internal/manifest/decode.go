package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func decodeStrict(data []byte, path string) (*Workspace, error) {
	switch ext := filepath.Ext(path); ext {
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		var ws Workspace
		if err := dec.Decode(&ws); err != nil {
			return nil, fmt.Errorf("parse yaml config: %w", err)
		}
		var extra yaml.Node
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse yaml config: only one document is allowed")
		}
		return &ws, nil
	default:
		return nil, fmt.Errorf("unsupported config format: %s (use .yaml or .yml)", ext)
	}
}

func extPrecedence(name string) int {
	if filepath.Ext(name) == ".yaml" {
		return 0
	}
	return 1
}
