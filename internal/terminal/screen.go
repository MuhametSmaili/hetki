package terminal

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

// ScreenLines prepares captured terminal text for display, keeping text and
// re-encoded SGR while discarding other terminal controls.
// Styles carry across rows, but each output line resets at both ends.
// One final record newline is ignored; blank rows are preserved.
func ScreenLines(text string) []string {
	if text == "" {
		return nil
	}

	text = strings.TrimSuffix(text, "\n")
	var lines []string
	var line strings.Builder
	var style uv.Style
	dirty := true
	finish := func() {
		line.WriteString(ansi.ResetStyle)
		lines = append(lines, line.String())
		line.Reset()
		dirty = true
	}

	printRune := func(r rune) {
		if dirty {
			line.WriteString(ansi.ResetStyle)
			if !style.IsZero() {
				line.WriteString(style.String())
			}
			dirty = false
		}
		line.WriteRune(r)
	}

	var p ansi.Parser
	p.SetParamsSize(32)
	p.SetDataSize(1) // Bound storage for discarded payloads.
	p.SetHandler(ansi.Handler{
		Print: printRune,
		Execute: func(b byte) {
			if b == '\n' {
				finish()
			}
		},
		HandleCsi: func(cmd ansi.Cmd, params ansi.Params) {
			if cmd == 'm' { // No private prefix or intermediate bytes.
				uv.ReadStyle(params, &style)
				dirty = true
			}
		},
	})

	feedScreenText(&p, text, printRune)
	finish()
	return lines
}

// Decode before the byte parser: UTF-8 continuation bytes must not become ST,
// and malformed UTF-8 must not consume a following newline or escape.
func feedScreenText(p *ansi.Parser, text string, printRune func(rune)) {
	for i, r := range text {
		switch {
		case text[i] < 0xa0: // ASCII and raw C1 controls.
			p.Advance(text[i])
		case r <= 0x9f: // UTF-8 encoded C1 controls.
			p.Advance(byte(r))
		default:
			switch p.State() {
			case parser.OscStringState, parser.DcsStringState, parser.ApcStringState, parser.PmStringState, parser.SosStringState:
				// Discard string payloads.
			default:
				p.Reset()
				printRune(r)
			}
		}
	}
}
