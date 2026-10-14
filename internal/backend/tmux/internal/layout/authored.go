package layout

import "fmt"

// ValidateAuthored accepts the existing manifest syntax, not observed geometry.
// In particular, legacy checksums and numeric ranges are not verified here.
func ValidateAuthored(text string) error {
	if text == "" || IsPreset(text) {
		return nil
	}
	if len(text) > maxLayoutBytes {
		return fmt.Errorf("unknown layout (exceeds %d bytes)", maxLayoutBytes)
	}
	if _, ok := parseLegacy(text, authoredSyntax); ok {
		return nil
	}
	return fmt.Errorf("unknown layout %q (named layout or saved layout dump)", text)
}

// IsPreset reports whether text is an exact supported layout name.
func IsPreset(text string) bool {
	switch text {
	case "even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled":
		return true
	default:
		return false
	}
}
