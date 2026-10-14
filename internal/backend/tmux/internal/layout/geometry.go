// Package layout handles tmux's serialized split-pane layouts.
package layout

import (
	"encoding/json"
	"strings"
)

const (
	// JSON observations are larger than the corresponding legacy manifest field.
	maxLayoutBytes = 16 << 10
	maxDepth       = 32
)

type cell struct {
	Kind     string  `json:"t"`
	Width    *uint32 `json:"w"`
	Height   *uint32 `json:"h"`
	X        *uint32 `json:"x"`
	Y        *uint32 `json:"y"`
	Children []cell  `json:"c"`
	PaneID   string  `json:"I"`
	Index    uint32  `json:"i"`
	Last     uint32  `json:"l"`
	Active   bool    `json:"a"`
}

// SameGeometry compares ordinary split layouts in legacy or tmux 3.8 JSON
// format. Pane IDs and selection metadata are not geometry. Invalid,
// unsupported or oversized layouts do not match.
func SameGeometry(first, second string) bool {
	a, ok := parse(first)
	if !ok {
		return false
	}

	b, ok := parse(second)
	return ok && sameCells(a, b)
}

func parse(text string) (cell, bool) {
	if len(text) == 0 || len(text) > maxLayoutBytes {
		return cell{}, false
	}

	if strings.HasPrefix(text, "{") {
		var document struct {
			Version int  `json:"V"`
			Root    cell `json:"L"`
		}
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.DisallowUnknownFields()
		if !validJSON(text) {
			return cell{}, false
		}

		if err := decoder.Decode(&document); err != nil {
			return cell{}, false
		}

		return document.Root, document.Version == 2 && validCell(document.Root, 0)
	}

	return parseLegacy(text)
}

func validCell(c cell, depth int) bool {
	if depth > maxDepth || c.Width == nil || c.Height == nil || c.X == nil || c.Y == nil {
		return false
	}

	if *c.Width == 0 || *c.Height == 0 {
		return false
	}

	if c.Kind == "p" {
		return len(c.Children) == 0
	}

	if (c.Kind != "h" && c.Kind != "v") || len(c.Children) < 2 {
		return false
	}

	for _, child := range c.Children {
		if !validCell(child, depth+1) {
			return false
		}
	}
	return true
}

func sameCells(a, b cell) bool {
	if a.Kind != b.Kind || *a.Width != *b.Width || *a.Height != *b.Height || *a.X != *b.X || *a.Y != *b.Y || len(a.Children) != len(b.Children) {
		return false
	}

	for i := range a.Children {
		if !sameCells(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}
