package tui

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette contains resolved picker colors. NoColor uses terminal defaults;
// two native selection colors use inverse normal text/background.
// A native Context follows normal/selected text; a native Emphasis follows Text.
type Palette struct {
	Text        color.Color
	Context     color.Color
	Command     color.Color
	Muted       color.Color
	Hint        color.Color
	Secondary   color.Color
	Emphasis    color.Color
	Active      color.Color
	Accent      color.Color
	Border      color.Color
	Divider     color.Color
	Working     color.Color
	Blocked     color.Color
	Idle        color.Color
	Success     color.Color
	Error       color.Color
	SelectionFG color.Color
	SelectionBG color.Color
}

// DefaultPalette keeps native text/background with dark-terminal accents.
// Paths remain italic in the theme.
func DefaultPalette() Palette {
	native := lipgloss.NoColor{}

	return Palette{
		Text:        native,
		Context:     native,
		Emphasis:    native,
		Secondary:   lipgloss.Color("#8b949e"),
		Muted:       lipgloss.Color("#6e7681"),
		Hint:        lipgloss.Color("#6e7681"),
		Active:      lipgloss.Color("#58a6ff"),
		Accent:      lipgloss.Color("#79c0ff"),
		Command:     lipgloss.Color("#39c5cf"),
		Border:      lipgloss.Color("#30363d"),
		Divider:     lipgloss.Color("#30363d"),
		SelectionFG: lipgloss.Color("#ffffff"),
		SelectionBG: lipgloss.Color("#1c2d44"),
		Working:     lipgloss.Color("#58a6ff"),
		Blocked:     lipgloss.Color("#d29922"),
		Idle:        lipgloss.Color("#3fb950"),
		Success:     lipgloss.Color("#3fb950"),
		Error:       lipgloss.Color("#ff7b72"),
	}
}

// Validate rejects incomplete programmatic palettes before opening the terminal.
func (p Palette) Validate() error {
	for _, field := range []struct {
		name  string
		value color.Color
	}{
		{"text", p.Text},
		{"context", p.Context},
		{"command", p.Command},
		{"muted", p.Muted},
		{"hint", p.Hint},
		{"secondary", p.Secondary},
		{"emphasis", p.Emphasis},
		{"active", p.Active},
		{"accent", p.Accent},
		{"border", p.Border},
		{"divider", p.Divider},
		{"working", p.Working},
		{"blocked", p.Blocked},
		{"idle", p.Idle},
		{"success", p.Success},
		{"error", p.Error},
		{"selection_fg", p.SelectionFG},
		{"selection_bg", p.SelectionBG},
	} {
		if field.value == nil {
			return fmt.Errorf("theme.%s is not configured", field.name)
		}
	}

	return nil
}

// ParseHexColor accepts exactly #RRGGBB, returning an opaque RGB override.
func ParseHexColor(value string) (color.RGBA, error) {
	if len(value) != 7 || value[0] != '#' {
		return color.RGBA{}, errors.New("color must use #RRGGBB")
	}

	var rgb [3]byte
	if _, err := hex.Decode(rgb[:], []byte(value[1:])); err != nil {
		return color.RGBA{}, fmt.Errorf("color must use #RRGGBB: %w", err)
	}

	return color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 255}, nil
}

func isTerminalDefault(c color.Color) bool {
	_, native := c.(lipgloss.NoColor)
	return native
}
