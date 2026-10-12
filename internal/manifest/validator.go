package manifest

import (
	"fmt"
	"strings"
	"unicode"
)

type ValidationError struct {
	Field   string
	Message string
}

func ToError(errs []ValidationError) error {
	if len(errs) == 0 {
		return nil
	}
	messages := make([]string, len(errs))
	for i, e := range errs {
		if e.Field != "" {
			messages[i] = fmt.Sprintf("%s: %s", e.Field, e.Message)
		} else {
			messages[i] = e.Message
		}
	}
	return fmt.Errorf("workspace validation failed:\n  - %s", strings.Join(messages, "\n  - "))
}

// Bounded-input limits enforced before any unbounded work.
const (
	MaxManifestBytes     = 1 << 20
	MaxSessions          = 64
	MaxWindowsPerSession = 64
	MaxPanesPerWindow    = 32
	MaxNameLength        = 255
	MaxPathLength        = 4096
	MaxCommandLength     = 4096
)

var namedLayouts = map[string]bool{
	"even-horizontal": true,
	"even-vertical":   true,
	"main-horizontal": true,
	"main-vertical":   true,
	"tiled":           true,
}

// Validate applies every manifest rule to a decoded workspace with indexed
// field paths. It runs on the raw decoded input, before normalization.
func Validate(ws *Workspace) []ValidationError {
	if ws == nil {
		return []ValidationError{{Message: "workspace is nil"}}
	}
	if len(ws.Sessions) == 0 {
		return []ValidationError{{Field: "sessions", Message: "at least one session is required"}}
	}
	if len(ws.Sessions) > MaxSessions {
		return []ValidationError{{Field: "sessions", Message: fmt.Sprintf("too many sessions (%d, max %d)", len(ws.Sessions), MaxSessions)}}
	}

	errs := make([]ValidationError, 0)
	seenSessions := make(map[string]bool, len(ws.Sessions))
	for i, sess := range ws.Sessions {
		errs = validateSession(sess, i, seenSessions, errs)
	}
	return errs
}

func validateSession(sess Session, i int, seen map[string]bool, errs []ValidationError) []ValidationError {
	at := func(field string) string { return fmt.Sprintf("sessions[%d].%s", i, field) }

	if strings.TrimSpace(sess.Name) == "" {
		return append(errs, ValidationError{Field: at("name"), Message: "session name cannot be empty"})
	}
	if len(sess.Name) > MaxNameLength {
		return append(errs, ValidationError{Field: at("name"), Message: nameTooLong("session")})
	}
	if err := checkName(sess.Name); err != "" {
		return append(errs, ValidationError{Field: at("name"), Message: err})
	}
	if seen[sess.Name] {
		return append(errs, ValidationError{Field: at("name"), Message: fmt.Sprintf("duplicate session name %q", sess.Name)})
	}
	seen[sess.Name] = true

	if len(sess.Root) > MaxPathLength {
		errs = append(errs, ValidationError{Field: at("root"), Message: "session root path is too long"})
	}
	if err := checkPath(sess.Root); err != "" {
		errs = append(errs, ValidationError{Field: at("root"), Message: err})
	}

	if len(sess.Windows) == 0 {
		return append(errs, ValidationError{Field: at("windows"), Message: "at least one window is required"})
	}
	if len(sess.Windows) > MaxWindowsPerSession {
		return append(errs, ValidationError{Field: at("windows"), Message: fmt.Sprintf("too many windows (%d, max %d)", len(sess.Windows), MaxWindowsPerSession)})
	}

	for j, win := range sess.Windows {
		errs = validateWindow(win, i, j, errs)
	}
	return errs
}

func validateWindow(win Window, i, j int, errs []ValidationError) []ValidationError {
	at := func(field string) string { return fmt.Sprintf("sessions[%d].windows[%d].%s", i, j, field) }

	if win.Index != nil {
		errs = append(errs, ValidationError{Field: at("index"), Message: "unsupported field: window index is managed by tmux; remove it"})
	}
	if len(win.Name) > MaxNameLength {
		errs = append(errs, ValidationError{Field: at("name"), Message: nameTooLong("window")})
	}
	if win.Name != "" {
		if err := checkName(win.Name); err != "" {
			errs = append(errs, ValidationError{Field: at("name"), Message: err})
		}
	}
	errs = validateWindowValues(win, at(""), errs)
	if len(win.Panes) > MaxPanesPerWindow {
		errs = append(errs, ValidationError{Field: at("panes"), Message: fmt.Sprintf("too many panes (%d, max %d)", len(win.Panes), MaxPanesPerWindow)})
	}

	zoomed := 0
	for k, pane := range win.Panes {
		atPane := func(field string) string { return fmt.Sprintf("%spanes[%d].%s", at(""), k, field) }
		errs = validatePane(pane, atPane(""), errs)
		if pane.Zoom {
			zoomed++
		}
	}
	if zoomed > 1 {
		errs = append(errs, ValidationError{Field: at("panes"), Message: fmt.Sprintf("%d panes with zoom=true (only one allowed per window)", zoomed)})
	}
	return errs
}

func validateWindowValues(win Window, at string, errs []ValidationError) []ValidationError {
	if len(win.Path) > MaxPathLength {
		errs = append(errs, ValidationError{Field: at + "path", Message: "window path is too long"})
	}
	if err := checkPath(win.Path); err != "" {
		errs = append(errs, ValidationError{Field: at + "path", Message: err})
	}
	if win.Command != "" && len(win.Panes) > 0 && win.Panes[0].Command != "" {
		errs = append(errs, ValidationError{Field: at + "command", Message: "window command conflicts with the first pane command; set only one"})
	}
	if win.Layout != "" && !namedLayouts[win.Layout] && !isLayoutDump(win.Layout) && len(win.Layout) <= MaxPathLength {
		errs = append(errs, ValidationError{Field: at + "layout", Message: fmt.Sprintf("unknown layout %q (named layout or saved layout dump)", win.Layout)})
	}
	if len(win.Layout) > MaxPathLength {
		errs = append(errs, ValidationError{Field: at + "layout", Message: "layout is too long"})
	}
	if len(win.Command) > MaxCommandLength {
		errs = append(errs, ValidationError{Field: at + "command", Message: "window command is too long"})
	}
	if err := checkCommand(win.Command); err != "" {
		errs = append(errs, ValidationError{Field: at + "command", Message: err})
	}
	return errs
}

func validatePane(pane Pane, at string, errs []ValidationError) []ValidationError {
	if pane.Split != nil {
		errs = append(errs, ValidationError{Field: at + "split", Message: "unsupported field: pane split is not implemented; remove it"})
	}
	if pane.Size != nil {
		errs = append(errs, ValidationError{Field: at + "size", Message: "unsupported field: pane size is not implemented; remove it"})
	}
	if len(pane.Path) > MaxPathLength {
		errs = append(errs, ValidationError{Field: at + "path", Message: "pane path is too long"})
	}
	if err := checkPath(pane.Path); err != "" {
		errs = append(errs, ValidationError{Field: at + "path", Message: err})
	}
	if len(pane.Command) > MaxCommandLength {
		errs = append(errs, ValidationError{Field: at + "command", Message: "pane command is too long"})
	}
	if err := checkCommand(pane.Command); err != "" {
		errs = append(errs, ValidationError{Field: at + "command", Message: err})
	}
	return errs
}

func checkName(name string) string {
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "name contains control characters"
	}
	return ""
}

func checkPath(path string) string {
	if strings.ContainsRune(path, '\x00') {
		return "path contains a NUL byte"
	}
	if strings.ContainsRune(path, '\n') {
		return "path contains a newline"
	}
	return ""
}

// Control bytes are terminal input, not shell command text: for example, a
// tab triggers completion and a newline submits early.
func checkCommand(cmd string) string {
	if strings.IndexFunc(cmd, unicode.IsControl) >= 0 {
		return "command contains control characters"
	}
	return ""
}

func isLayoutDump(layout string) bool {
	checksum, body, ok := strings.Cut(layout, ",")
	if !ok || len(checksum) != 4 || strings.IndexFunc(checksum, func(r rune) bool {
		return !strings.ContainsRune("0123456789abcdefABCDEF", r)
	}) >= 0 {
		return false
	}
	i := 0
	return parseLayoutCell(body, &i) && i == len(body)
}

func parseLayoutCell(layout string, i *int) bool {
	if !parseLayoutNumber(layout, i) || !takeLayoutByte(layout, i, 'x') ||
		!parseLayoutNumber(layout, i) || !takeLayoutByte(layout, i, ',') ||
		!parseLayoutNumber(layout, i) || !takeLayoutByte(layout, i, ',') ||
		!parseLayoutNumber(layout, i) || *i >= len(layout) {
		return false
	}

	if layout[*i] == ',' {
		(*i)++
		return parseLayoutNumber(layout, i)
	}

	open := layout[*i]
	if open != '{' && open != '[' {
		return false
	}
	close := byte('}')
	if open == '[' {
		close = ']'
	}
	(*i)++
	children := 0
	for {
		if !parseLayoutCell(layout, i) {
			return false
		}
		children++
		if *i >= len(layout) {
			return false
		}
		if layout[*i] == close {
			(*i)++
			return children >= 2
		}
		if !takeLayoutByte(layout, i, ',') {
			return false
		}
	}
}

func parseLayoutNumber(layout string, i *int) bool {
	start := *i
	for *i < len(layout) && layout[*i] >= '0' && layout[*i] <= '9' {
		(*i)++
	}
	return *i > start
}

func takeLayoutByte(layout string, i *int, want byte) bool {
	if *i >= len(layout) || layout[*i] != want {
		return false
	}
	(*i)++
	return true
}

func nameTooLong(kind string) string {
	return fmt.Sprintf("%s name exceeds %d characters", kind, MaxNameLength)
}
