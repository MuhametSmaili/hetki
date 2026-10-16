package manifest

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Marshal validates with the required pure layout policy, then writes the
// authored representation rather than its normalized copy.
func Marshal(workspace *Workspace, path string, validateLayout func(string) error) ([]byte, error) {
	if _, err := Normalize(workspace, validateLayout); err != nil {
		return nil, err
	}

	ext := filepath.Ext(expandPath(path))
	if ext != ".yaml" && ext != ".yml" {
		return nil, fmt.Errorf("unsupported format: %s (use .yaml or .yml)", ext)
	}
	data, err := yaml.Marshal(workspace)
	if err != nil {
		return nil, fmt.Errorf("marshal yaml: %w", err)
	}
	if len(data) > MaxManifestBytes {
		return nil, fmt.Errorf("marshaled manifest exceeds %d bytes", MaxManifestBytes)
	}
	return data, nil
}
