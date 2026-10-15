package manifest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "hetki"), nil
}

type Loader interface {
	Load() (*Workspace, error)
}

type FileLoader struct {
	Path           string
	validateLayout func(string) error
}

// NewFileLoader binds a required pure, bounded layout validator to file loading.
func NewFileLoader(path string, validateLayout func(string) error) *FileLoader {
	return &FileLoader{Path: path, validateLayout: validateLayout}
}

func (l *FileLoader) Load() (*Workspace, error) {
	if l.validateLayout == nil {
		return nil, ErrLayoutValidatorRequired
	}

	extendedPath := expandPath(l.Path)
	file, err := os.Open(extendedPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	defer file.Close()
	// Bounded read: the size cap must apply before the file is fully read.
	data, err := io.ReadAll(io.LimitReader(file, MaxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	return Parse(data, extendedPath, l.validateLayout)
}

// Parse is the single manifest pipeline: bounded read → strict decode →
// validate with field paths → normalize (first-pane compilation, root/path
// inheritance). Every workspace consumer (start and save verification) goes
// through here. validateLayout is required and must be pure and bounded.
func Parse(data []byte, path string, validateLayout func(string) error) (*Workspace, error) {
	if validateLayout == nil {
		return nil, ErrLayoutValidatorRequired
	}

	if len(data) > MaxManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", MaxManifestBytes)
	}

	raw, err := decodeStrict(data, path)
	if err != nil {
		return nil, err
	}
	return Normalize(raw, validateLayout)
}

// Normalize validates a workspace and returns a normalized copy without changing
// the input. It resolves directories and compiles first-pane commands. The layout
// validator is required and must be pure and bounded, as for Parse.
func Normalize(raw *Workspace, validateLayout func(string) error) (*Workspace, error) {
	if errs := Validate(raw, validateLayout); len(errs) > 0 {
		return nil, ToError(errs)
	}

	normalized, err := normalize(raw)
	if err != nil {
		return nil, err
	}
	if errs := validateNormalized(normalized); len(errs) > 0 {
		return nil, ToError(errs)
	}
	return normalized, nil
}

// normalize compiles directory inheritance and commands without changing the
// authored workspace. Relative roots and rootless windows use the caller's
// working directory, not the manifest file's location.
func normalize(cfg *Workspace) (*Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}

	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	out := &Workspace{Sessions: make([]Session, len(cfg.Sessions))}

	for i, sess := range cfg.Sessions {
		base := cwd
		if sess.Root != "" {
			sess.Root, err = resolveSessionRoot(sess.Root, cwd)
			if err != nil {
				return nil, fmt.Errorf("sessions[%d].root: %w", i, err)
			}
			base = sess.Root
		}
		windows := make([]Window, len(sess.Windows))
		for j, window := range sess.Windows {
			windows[j], err = normalizeWindow(window, base)
			if err != nil {
				return nil, fmt.Errorf("sessions[%d].windows[%d].%w", i, j, err)
			}
		}
		sess.Windows = windows
		out.Sessions[i] = sess
	}
	return out, nil
}

func normalizeWindow(w Window, base string) (Window, error) {
	directory, err := resolveDirectory(w.Path, base)
	if err != nil {
		return Window{}, fmt.Errorf("path: %w", err)
	}
	w.Path = directory
	w.Panes, err = compilePanes(w)
	if err != nil {
		return Window{}, err
	}

	// All panes resolve against the declared window base; only creation and
	// comparison use the first pane's final directory.
	w.Path = w.Panes[0].Path
	w.Command = ""
	if w.Name == "" {
		w.Name = inferNameFromPath(w.Path)
	}
	return w, nil
}

func compilePanes(w Window) ([]Pane, error) {
	if len(w.Panes) == 0 {
		return []Pane{{Path: w.Path, Command: w.Command}}, nil
	}
	panes := make([]Pane, len(w.Panes))
	for k, p := range w.Panes {
		path, err := resolveDirectory(p.Path, w.Path)
		if err != nil {
			return nil, fmt.Errorf("panes[%d].path: %w", k, err)
		}
		p.Path = path
		if k == 0 && p.Command == "" {
			p.Command = w.Command
		}
		panes[k] = p
	}
	return panes, nil
}

// validateNormalized re-checks inferred names and resolved directories against
// the raw-input limits, including values produced by symlink resolution.
func validateNormalized(ws *Workspace) []ValidationError {
	var errs []ValidationError
	for i, sess := range ws.Sessions {
		sessionAt := fmt.Sprintf("sessions[%d]", i)
		if len(sess.Root) > MaxPathLength {
			errs = append(errs, ValidationError{Field: sessionAt + ".root", Message: "resolved session root path is too long"})
		}
		if err := checkPath(sess.Root); err != "" {
			errs = append(errs, ValidationError{Field: sessionAt + ".root", Message: err})
		}
		for j, w := range sess.Windows {
			at := fmt.Sprintf("%s.windows[%d]", sessionAt, j)
			errs = append(errs, validateNormalizedWindow(w, at)...)
		}
	}
	return errs
}

func validateNormalizedWindow(w Window, at string) []ValidationError {
	var errs []ValidationError
	if w.Name == "" {
		errs = append(errs, ValidationError{Field: at + ".name", Message: "inferred window name cannot be empty"})
	} else if len(w.Name) > MaxNameLength {
		errs = append(errs, ValidationError{Field: at + ".name", Message: nameTooLong("inferred window")})
	}
	if err := checkName(w.Name); err != "" {
		errs = append(errs, ValidationError{Field: at + ".name", Message: err + " (inferred from path)"})
	}
	if len(w.Path) > MaxPathLength {
		errs = append(errs, ValidationError{Field: at + ".path", Message: "resolved window path is too long"})
	}
	if err := checkPath(w.Path); err != "" {
		errs = append(errs, ValidationError{Field: at + ".path", Message: err})
	}

	for k, pane := range w.Panes {
		paneAt := fmt.Sprintf("%s.panes[%d].path", at, k)
		if len(pane.Path) > MaxPathLength {
			errs = append(errs, ValidationError{Field: paneAt, Message: "resolved pane path is too long"})
		}
		if err := checkPath(pane.Path); err != "" {
			errs = append(errs, ValidationError{Field: paneAt, Message: err})
		}
	}
	return errs
}

func inferNameFromPath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(p))
}

func expandPath(p string) string {
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p)

	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// ScanWorkspaces lists YAML workspaces in dir. For duplicate basename
// variants, .yaml wins over .yml.
func ScanWorkspaces(dir string) (map[string]string, error) {
	expandedDir := expandPath(dir)
	entries, err := os.ReadDir(expandedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	paths := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !hasValidExt(name) {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		path := filepath.Join(expandedDir, name)
		if existing, ok := paths[base]; !ok || extPrecedence(name) < extPrecedence(existing) {
			paths[base] = path
		}
	}
	return paths, nil
}
