// Package foreground provides best-effort process-provided names for terminal
// foreground jobs. Names are display hints, not authenticated identity or work
// status. Inspection is local to the caller's OS process namespace.
package foreground

import (
	"context"
	"math"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxProgramBytes = 64 * 1024

// Name returns an optional naming hint for the terminal foreground job of
// anchorPID. It names the foreground process-group leader; helper processes in
// the same group do not suppress the hint. Unavailable, unsupported or changed
// evidence returns an empty string. Reads are bounded and process relationships
// are rechecked, but inspection is not atomic. Darwin is currently supported;
// other platforms return no hint rather than approximate foreground-group
// membership. Context cancellation is checked between native reads; it cannot
// interrupt an in-flight syscall.
func Name(ctx context.Context, anchorPID int) string {
	if anchorPID <= 0 || anchorPID > math.MaxInt32 || ctx.Err() != nil {
		return ""
	}
	return processName(ctx, anchorPID)
}

// argv-zero is a process-provided display hint, not authenticated identity.
// Never infer an application from script paths, other arguments or metadata.
// Assignments and multi-token strings are not program names.
func processProgramName(argvZero string) string {
	if !utf8.ValidString(argvZero) || strings.ContainsAny(argvZero, "\\=") || strings.IndexFunc(argvZero, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return ""
	}
	name := path.Base(argvZero)
	if name == "." || name == ".." || name == "/" || len(name) > 64 || strings.HasPrefix(name, "-") {
		return ""
	}
	return name
}
