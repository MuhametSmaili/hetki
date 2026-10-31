package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Theme preserves optional hex color overrides independently of UI styling types.
type Theme struct {
	Text        *string
	Context     *string
	Command     *string
	Muted       *string
	Hint        *string
	Secondary   *string
	Emphasis    *string
	Active      *string
	Accent      *string
	Border      *string
	Divider     *string
	Working     *string
	Blocked     *string
	Idle        *string
	Success     *string
	Error       *string
	SelectionFG *string
	SelectionBG *string
}

func decodeTheme(node *yaml.Node) (*Theme, error) {
	if err := checkMapping(node, "theme"); err != nil {
		return nil, err
	}

	var result Theme
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		target := themeField(&result, key)
		if target == nil {
			return nil, fmt.Errorf("theme has an unknown field")
		}

		if *target != nil {
			return nil, fmt.Errorf("duplicate theme.%s field", key)
		}

		if err := checkString(value, "theme."+key); err != nil {
			return nil, err
		}

		*target = new(value.Value)
	}

	return &result, nil
}

func themeField(theme *Theme, key string) **string {
	switch key {
	case "text":
		return &theme.Text
	case "context":
		return &theme.Context
	case "command":
		return &theme.Command
	case "muted":
		return &theme.Muted
	case "hint":
		return &theme.Hint
	case "secondary":
		return &theme.Secondary
	case "emphasis":
		return &theme.Emphasis
	case "active":
		return &theme.Active
	case "accent":
		return &theme.Accent
	case "border":
		return &theme.Border
	case "divider":
		return &theme.Divider
	case "working":
		return &theme.Working
	case "blocked":
		return &theme.Blocked
	case "idle":
		return &theme.Idle
	case "success":
		return &theme.Success
	case "error":
		return &theme.Error
	case "selection_fg":
		return &theme.SelectionFG
	case "selection_bg":
		return &theme.SelectionBG
	default:
		return nil
	}
}
