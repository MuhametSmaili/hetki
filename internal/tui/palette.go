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

// DefaultPalette keeps native text/background with soft, dark-terminal accents
// inspired by Catppuccin and Rosé Pine. Paths remain italic in the theme.
func DefaultPalette() Palette {
	native := lipgloss.NoColor{}

	return Palette{
		Text:        native,
		Context:     native,
		Command:     lipgloss.Color("#9ccfd8"),
		Muted:       lipgloss.Color("#87839f"),
		Hint:        lipgloss.Color("#87839f"),
		Secondary:   lipgloss.Color("#a4a1b7"),
		Emphasis:    native,
		Active:      lipgloss.Color("#cba6f7"),
		Accent:      lipgloss.Color("#89b4fa"),
		Border:      lipgloss.Color("#6e6a86"),
		Divider:     lipgloss.Color("#6e6a86"),
		Working:     lipgloss.Color("#89b4fa"),
		Blocked:     lipgloss.Color("#f6c177"),
		Idle:        lipgloss.Color("#a6da95"),
		Success:     lipgloss.Color("#a6da95"),
		Error:       lipgloss.Color("#eb6f92"),
		SelectionFG: lipgloss.Color("#e0def4"),
		SelectionBG: lipgloss.Color("#39374a"),
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
