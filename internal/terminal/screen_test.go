package terminal

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

var screenSGR = regexp.MustCompile("\x1b\\[[0-9;:]*m")

func assertSafeScreenLine(t *testing.T, line string) {
	t.Helper()
	require.True(t, utf8.ValidString(line))
	require.True(t, strings.HasPrefix(line, ansi.ResetStyle), "line inherits external styles: %q", line)
	require.True(t, strings.HasSuffix(line, ansi.ResetStyle), "line does not reset: %q", line)
	require.Equal(t, Sanitize(line), screenSGR.ReplaceAllString(line, ""), "non-SGR control in %q", line)
}

func TestScreenLinesCutUnicode(t *testing.T) {
	const text = "中👨‍👩‍👧e\u0301!"
	lines := ScreenLines("\x1b[31;44m" + text + "\n" + text + "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		for width, want := range []string{"", "", "中", "中", "中👨‍👩‍👧", "中👨‍👩‍👧e\u0301", text} {
			cut := Cut(line, 0, width)
			require.LessOrEqual(t, Width(cut), width)
			require.Equal(t, want, Sanitize(cut))
			if width > 0 {
				assertSafeScreenLine(t, cut)
			}
		}
		require.Equal(t, "👨‍👩‍👧e\u0301", Sanitize(Cut(line, 2, 5)))
	}
}

func FuzzScreenLines(f *testing.F) {
	for _, text := range []string{
		"", "\n\n", "\x1b[31m中👨‍👩‍👧e\u0301\nred\x1b[m",
		"\x1b]52;c;secret\a\u009b2J\x1bPqsecret\x1b\\",
		"\x1b[38:2::1:2:3mcolor\x1b[0", "\x1b[" + strings.Repeat("9", 100) + "m",
		"\xff\xc2\x1b[31m\xf0\x9f", "\x1b[" + strings.Repeat("1;", 64) + "m",
		"a\x1b]0;ÜSECRET\ab", "a\x1bPqÜSECRET\x1b\\b", "a\xc2\nb\n",
	} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 1<<20 {
			t.Skip()
		}
		lines := ScreenLines(text)
		for _, line := range lines {
			assertSafeScreenLine(t, line)
		}
		require.Equal(t, lines, ScreenLines(strings.Join(lines, "\n")))
		payload := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, text)
		for _, prefix := range []string{"\x1b]0;", "\x1b_G"} {
			require.Equal(t, []string{ansi.ResetStyle + "ab" + ansi.ResetStyle}, ScreenLines("a"+prefix+payload+"\x1b\\b"))
		}
	})
}

func TestScreenLinesLargeDiscardedPayload(t *testing.T) {
	text := "before\x1b]52;c;" + strings.Repeat("x", (1<<20)-64) + "\x1b\\after\n"
	require.Equal(t, []string{ansi.ResetStyle + "beforeafter" + ansi.ResetStyle}, ScreenLines(text))
}

func BenchmarkScreenLines(b *testing.B) {
	text := strings.Repeat("\x1b[38;2;12;34;56;48;5;234m"+strings.Repeat("中👨‍👩‍👧e\u0301 ", 208)+"\x1b[m\n", 200)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for b.Loop() {
		ScreenLines(text)
	}
}

func TestScreenLinesStyles(t *testing.T) {
	cells := func(text string) []uv.Line {
		buf := uv.NewScreenBuffer(8, 4)
		uv.NewStyledString(text).Draw(buf, buf.Bounds())
		return buf.Lines
	}
	for name, text := range map[string]string{
		"carry through blank line":       "\x1b[1;31;44ma\n\n b\n",
		"default colors":                 "\x1b[31;44ma\x1b[39mb\n\x1b[49mc",
		"empty reset":                    "\x1b[31ma\x1b[mb\nc",
		"zero reset":                     "\x1b[31ma\x1b[0m\nb",
		"attributes":                     "\x1b[1;2;3;4;5;6;7;8;9ma\n\x1b[22;23;24;25;27;28;29mb",
		"indexed and RGB":                "\x1b[38;5;196;48;2;12;34;56ma\nb",
		"colon colors":                   "\x1b[38:2::1:2:3;48:5:255ma\nb",
		"non-SGR and unknown attributes": "\x1b[31m\x1b[?32m\x1b[33!m\x1b[999ma",
	} {
		t.Run(name, func(t *testing.T) {
			lines := ScreenLines(text)
			for _, line := range lines {
				assertSafeScreenLine(t, line)
			}
			require.Equal(t, cells(text), cells(strings.Join(lines, "\n")))
		})
	}
	const reset = ansi.ResetStyle
	require.Equal(t, []string{reset + "\x1b[31ma" + reset, reset + "\x1b[31;44mb" + reset}, ScreenLines("\x9b31ma\n\u009b44mb"))
}

func TestScreenLinesSafeContent(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want []string
	}{
		{"empty capture", "", nil},
		{"one empty record", "\n", []string{""}},
		{"layout", "  中👨‍👩‍👧e\u0301  \n\n last \n\n", []string{"  中👨‍👩‍👧e\u0301  ", "", " last ", ""}},
		{"unterminated record", "first\nlast", []string{"first", "last"}},
		{"OSC", "a\x1b]0;title\ab\x1b]52;c;clipboard\x1b\\c\x1b]8;;https://example.invalid\alink\x1b]8;;\x1b\\d", []string{"abclinkd"}},
		{"program status", "a" + ansi.SetProgramStatus(ansi.ProgramStatus{State: ansi.ProgramStateWorking, App: "charm"}) + "b", []string{"ab"}},
		{"strings", "a\x1bP1;2qDCS\x1b\\b\x1b_Gimage\x1b\\c\x1b^private\x1b\\d\x1bXsos\x1b\\e", []string{"abcde"}},
		{"ESC and CSI", "a\x1bc\x1b7\x1b8\x1b(Bb\x1b[2J\x1b[999;999H\x1b[?1049h\x1b[6n\x1b[!mc", []string{"abc"}},
		{"C0", "a\x00\a\b\t\v\f\r\x0e\x0f\x18\x1a\x7fb\nc", []string{"ab", "c"}},
		{"raw C1", "a\x80\x85\x9b2Jb\x9d52;c;clipboard\x9cc\x90qsecret\x9cd\x9fimage\x9ce", []string{"abcde"}},
		{"UTF-8 C1", "a\u0080\u0085\u009b2Jb\u009d52;c;clipboard\u009cc\u0090qsecret\u009cd\u009fimage\u009ce", []string{"abcde"}},
		{"UTF-8 in OSC", "a\x1b]0;ÜSECRET\ab", []string{"ab"}},
		{"UTF-8 in DCS", "a\x1bPqÜSECRET\x1b\\b", []string{"ab"}},
		{"UTF-8 in APC/PM/SOS", "a\x1b_GλÜsecret\x1b\\b\x1b^λÜsecret\x1b\\c\x1bXλÜsecret\x1b\\d", []string{"abcd"}},
		{"invalid UTF-8 before newline", "a\xc2\nb\n", []string{"a�", "b"}},
		{"truncated UTF-8", "a\xf0\x9f", []string{"a�"}},
		{"incomplete ESC", "ok\x1b", []string{"ok"}},
		{"incomplete CSI", "ok\x1b[38;2;255", []string{"ok"}},
		{"incomplete OSC", "ok\x1b]52;c;secret\nmore", []string{"ok"}},
		{"incomplete DCS", "ok\x1bPqsecret", []string{"ok"}},
		{"incomplete APC", "ok\x1b_Gsecret", []string{"ok"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lines := ScreenLines(tt.text)
			var plain []string
			for _, line := range lines {
				assertSafeScreenLine(t, line)
				plain = append(plain, Sanitize(line))
			}
			require.Equal(t, tt.want, plain)
		})
	}
}
