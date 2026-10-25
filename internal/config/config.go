// Package config reads optional picker preferences independently of runtime UI types.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxBytes = 64 * 1024
const maxDepth = 16

// Config preserves omitted preferences so the application can supply defaults.
type Config struct{ UI UI }

// UI contains only explicitly configured startup choices.
type UI struct {
	View    *string
	Scope   *string
	Preview *bool
}

// Decode accepts one YAML document of at most 64 KiB, reading one extra byte
// to detect overflow even when the reader grows.
func Decode(reader io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}
	if len(data) > maxBytes {
		return Config{}, fmt.Errorf("configuration exceeds 64 KiB")
	}

	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, nil
		}
		return Config{}, &decodeError{cause: err}
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, &decodeError{cause: err}
		}
		return Config{}, fmt.Errorf("configuration must contain exactly one document")
	}

	root := document.Content[0]
	if err := checkStructure(root, 1); err != nil {
		return Config{}, err
	}
	return decodeDocument(root)
}

// Parser diagnostics can contain source text. Keep the cause available without
// displaying user-supplied document fragments.
type decodeError struct{ cause error }

func (e *decodeError) Error() string { return "invalid configuration YAML" }
func (e *decodeError) Unwrap() error { return e.cause }

func checkStructure(node *yaml.Node, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("configuration exceeds YAML depth 16")
	}
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("configuration must not contain aliases")
	}

	for _, child := range node.Content {
		if err := checkStructure(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func checkMapping(node *yaml.Node, field string) error {
	if node.Kind != yaml.MappingNode || node.Tag != "!!map" || node.Style&yaml.TaggedStyle != 0 {
		return fmt.Errorf("%s must be a mapping", field)
	}

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Style&yaml.TaggedStyle != 0 {
			return fmt.Errorf("%s keys must be strings", field)
		}
	}
	return nil
}

func decodeDocument(root *yaml.Node) (Config, error) {
	if err := checkMapping(root, "configuration"); err != nil {
		return Config{}, err
	}

	var result Config
	seen := false
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "ui" {
			return Config{}, fmt.Errorf("configuration has an unknown field")
		}
		if seen {
			return Config{}, fmt.Errorf("duplicate ui field")
		}
		seen = true

		ui, err := decodeUI(value)
		if err != nil {
			return Config{}, err
		}
		result.UI = ui
	}
	return result, nil
}

func decodeUI(node *yaml.Node) (UI, error) {
	if err := checkMapping(node, "ui"); err != nil {
		return UI{}, err
	}

	var result UI
	for i := 0; i < len(node.Content); i += 2 {
		if err := decodeUIField(node.Content[i].Value, node.Content[i+1], &result); err != nil {
			return UI{}, err
		}
	}
	return result, nil
}

func decodeUIField(key string, node *yaml.Node, result *UI) error {
	var target **string
	switch key {
	case "view":
		target = &result.View
	case "scope":
		target = &result.Scope
	case "preview":
		if result.Preview != nil {
			return fmt.Errorf("duplicate ui.preview field")
		}
		value, err := decodeBoolean(node, "ui.preview")
		result.Preview = value
		return err
	default:
		return fmt.Errorf("ui has an unknown field")
	}

	field := "ui." + key
	if *target != nil {
		return fmt.Errorf("duplicate %s field", field)
	}
	if err := checkString(node, field); err != nil {
		return err
	}
	value := node.Value
	*target = &value
	return nil
}

func decodeBoolean(node *yaml.Node, field string) (*bool, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" || node.Style&yaml.TaggedStyle != 0 {
		return nil, fmt.Errorf("%s must be an untagged boolean", field)
	}
	value := strings.EqualFold(node.Value, "true")
	return &value, nil
}

func checkString(node *yaml.Node, field string) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" || node.Style&yaml.TaggedStyle != 0 {
		return fmt.Errorf("%s must be an untagged string", field)
	}
	return nil
}
