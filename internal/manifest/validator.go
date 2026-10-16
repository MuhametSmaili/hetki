package manifest

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrLayoutValidatorRequired reports missing offline validation configuration.
var ErrLayoutValidatorRequired = errors.New("layout validation policy is required")

// ValidationError identifies a manifest field and retains any policy error cause.
type ValidationError struct {
	Field   string
	Message string
	Cause   error
}

func (e ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

func (e ValidationError) Unwrap() error { return e.Cause }

type validationErrors []ValidationError

func (errs validationErrors) Error() string {
	messages := make([]string, len(errs))
	for i, err := range errs {
		messages[i] = err.Error()
	}
	return "workspace validation failed:\n  - " + strings.Join(messages, "\n  - ")
}

func (errs validationErrors) Unwrap() []error {
	causes := make([]error, len(errs))
	for i, err := range errs {
		causes[i] = err
	}
	return causes
}

func ToError(errs []ValidationError) error {
	if len(errs) == 0 {
		return nil
	}
	return append(validationErrors(nil), errs...)
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

// Validate applies every manifest rule to a decoded workspace with indexed
// field paths. It runs on raw input before normalization. validateLayout is a
// required pure policy; omitted and oversized layouts are not passed to it.
func Validate(ws *Workspace, validateLayout func(string) error) []ValidationError {
	if validateLayout == nil {
		return []ValidationError{{Message: ErrLayoutValidatorRequired.Error(), Cause: ErrLayoutValidatorRequired}}
	}
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
		errs = validateSession(sess, i, seenSessions, errs, validateLayout)
	}
	return errs
}

func validateSession(sess Session, i int, seen map[string]bool, errs []ValidationError, validateLayout func(string) error) []ValidationError {
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
		errs = validateWindow(win, i, j, errs, validateLayout)
	}
	return errs
}

func validateWindow(win Window, i, j int, errs []ValidationError, validateLayout func(string) error) []ValidationError {
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
	errs = validateWindowValues(win, at(""), errs, validateLayout)
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

func validateWindowValues(win Window, at string, errs []ValidationError, validateLayout func(string) error) []ValidationError {
	if len(win.Path) > MaxPathLength {
		errs = append(errs, ValidationError{Field: at + "path", Message: "window path is too long"})
	}
	if err := checkPath(win.Path); err != "" {
		errs = append(errs, ValidationError{Field: at + "path", Message: err})
	}
	if win.Command != "" && len(win.Panes) > 0 && win.Panes[0].Command != "" {
		errs = append(errs, ValidationError{Field: at + "command", Message: "window command conflicts with the first pane command; set only one"})
	}
	if len(win.Layout) > MaxPathLength {
		errs = append(errs, ValidationError{Field: at + "layout", Message: "layout is too long"})
	} else if win.Layout != "" {
		if err := validateLayout(win.Layout); err != nil {
			errs = append(errs, ValidationError{Field: at + "layout", Message: err.Error(), Cause: err})
		}
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

func nameTooLong(kind string) string {
	return fmt.Sprintf("%s name exceeds %d characters", kind, MaxNameLength)
}
