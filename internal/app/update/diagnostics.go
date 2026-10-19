package update

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/MuhametSmaili/hetki/internal/terminal"
	"github.com/charmbracelet/x/ansi"
)

const maxDiagnosticBytes = 512

var (
	diagnosticCredentials = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|[a-z0-9_]*(token|secret|password|passwd|api[_-]?key|credential)[a-z0-9_]*)["']?\s*[:=]`)
	diagnosticURL         = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s"'<>]+`)
)

// summarizedFailure sanitizes and bounds display without discarding failure causes.
type summarizedFailure struct {
	cause   error
	summary string
}

func (e *summarizedFailure) Error() string { return e.summary }
func (e *summarizedFailure) Unwrap() error { return e.cause }

func summarizeFailure(err error) error {
	message := err.Error()
	summary := processDiagnostic([]byte(message), false)
	if summary == message {
		return err
	}
	return &summarizedFailure{cause: err, summary: summary}
}

func commandFailure(err error, stderr *boundedBuffer) error {
	detail := processDiagnostic(stderr.Bytes(), stderr.truncated)
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

func processDiagnostic(data []byte, truncated bool) string {
	text := string(data)
	if truncated {
		// A capture boundary may split a credential. Never display that line.
		text = text[:strings.LastIndexByte(text, '\n')+1]
	}

	tokens := []string{os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN")}
	const suffix = " [truncated]"
	limit := maxDiagnosticBytes - len(suffix)
	summary := ""

	// Keep the final explanation rather than verbose build-progress prefixes.
	for len(text) > 0 {
		text = strings.TrimRight(text, "\n")
		start := strings.LastIndexByte(text, '\n') + 1
		line := strings.TrimSpace(text[start:])
		text = text[:max(0, start-1)]
		if line == "" {
			continue
		}
		line = safeDiagnosticLine(line, tokens)
		if line == "" {
			continue
		}
		if summary != "" {
			line += "; "
		}
		remaining := limit - len(summary)
		if len(line) > remaining {
			summary = diagnosticTail(line, remaining) + summary
			truncated = true
			break
		}
		summary = line + summary
	}

	if truncated {
		summary += suffix
	}
	return strings.TrimSpace(summary)
}

func safeDiagnosticLine(line string, tokens []string) string {
	// Preserve whitespace until credential syntax has been checked.
	line = ansi.Strip(line)
	for _, token := range tokens {
		if token != "" && strings.Contains(line, ansi.Strip(token)) {
			return "[credentials omitted]"
		}
	}

	if diagnosticCredentials.MatchString(line) {
		return "[credentials omitted]"
	}

	// URLs can contain credentials in userinfo, paths or signed query strings.
	return terminal.Sanitize(diagnosticURL.ReplaceAllString(line, "[URL omitted]"))
}

func diagnosticTail(text string, maxBytes int) string {
	start := len(text) - maxBytes
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return text[start:]
}
