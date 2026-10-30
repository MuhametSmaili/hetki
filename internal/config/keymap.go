package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func decodeKeymap(node *yaml.Node) (map[string]map[string][]string, error) {
	if err := checkMapping(node, "keymap"); err != nil {
		return nil, err
	}

	modes := make(map[string]map[string][]string, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		mode := node.Content[i].Value
		if _, exists := modes[mode]; exists {
			return nil, fmt.Errorf("duplicate keymap mode")
		}

		actions, err := decodeKeymapActions(node.Content[i+1])
		if err != nil {
			return nil, err
		}
		modes[mode] = actions
	}
	return modes, nil
}

func decodeKeymapActions(node *yaml.Node) (map[string][]string, error) {
	if err := checkMapping(node, "keymap mode"); err != nil {
		return nil, err
	}

	actions := make(map[string][]string, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		action := node.Content[i].Value
		if _, exists := actions[action]; exists {
			return nil, fmt.Errorf("duplicate keymap action")
		}

		keys, err := decodeKeymapKeys(node.Content[i+1])
		if err != nil {
			return nil, err
		}
		actions[action] = keys
	}
	return actions, nil
}

func decodeKeymapKeys(node *yaml.Node) ([]string, error) {
	if node.Kind != yaml.SequenceNode || node.Tag != "!!seq" || node.Style&yaml.TaggedStyle != 0 {
		return nil, fmt.Errorf("keymap action must be a list of strings")
	}

	keys := make([]string, len(node.Content))
	for i, item := range node.Content {
		if err := checkString(item, "keymap key"); err != nil {
			return nil, err
		}
		keys[i] = item.Value
	}
	return keys, nil
}
