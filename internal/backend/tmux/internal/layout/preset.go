package layout

import "errors"

// Preset chooses a readable layout name, not an exact geometry snapshot.
// Missing observations remain omitted; invalid or unsupported layouts fail.
func Preset(text string) (string, error) {
	if text == "" || IsPreset(text) {
		return text, nil
	}

	root, ok := parse(text)
	if !ok {
		return "", errors.New("invalid or unsupported tmux layout")
	}

	if root.Kind == "p" {
		return "", nil
	}

	if hasOnlyPanes(root) {
		if root.Kind == "h" {
			return "even-horizontal", nil
		}
		return "even-vertical", nil
	}

	if len(root.Children) == 2 && root.Children[0].Kind == "p" && hasOnlyPanes(root.Children[1]) {
		if root.Kind == "v" && root.Children[1].Kind == "h" {
			return "main-horizontal", nil
		}
		if root.Kind == "h" && root.Children[1].Kind == "v" {
			return "main-vertical", nil
		}
	}
	return "tiled", nil
}

func hasOnlyPanes(c cell) bool {
	for _, child := range c.Children {
		if child.Kind != "p" {
			return false
		}
	}
	return true
}
