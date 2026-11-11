// Package actions runs user-authored selection flows independently of the UI.
package actions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
)

const (
	MaxItems   = 10_000
	maxSteps   = 32
	maxActions = 128
	maxOutput  = 4 * 1024 * 1024
	maxRecord  = 64 * 1024
)

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// Catalog is immutable after loading. Each recipe ends in one explicit mutation.
type Catalog struct {
	Version int               `yaml:"version"`
	Actions map[string]Recipe `yaml:"actions"`
}

type Recipe struct {
	Label string   `yaml:"label"`
	Scope []string `yaml:"scope"`
	Steps []Step   `yaml:"steps"`
}

type Step struct {
	Select string      `yaml:"select"`
	Title  string      `yaml:"title"`
	Source *SourceSpec `yaml:"source"`
	Run    *Command    `yaml:"run"`
}

type SourceSpec struct {
	Builtin string   `yaml:"builtin"`
	Exec    *Command `yaml:"exec"`
	Items   []Item   `yaml:"items"`
	Session string   `yaml:"session"`
	Window  string   `yaml:"window"`
}

// Command is executed directly; shell syntax requires an explicit shell argv.
type Command struct {
	Argv    []string `yaml:"argv"`
	Timeout string   `yaml:"timeout"`
}

// Item separates display text from extension-owned JSON-compatible values.
// target is private provenance, supplied only by the native inventory source.
type Item struct {
	ID          string         `yaml:"id" json:"id"`
	Label       string         `yaml:"label" json:"label"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Value       map[string]any `yaml:"value,omitempty" json:"value,omitempty"`
	target      *backend.Target
}

func (c Catalog) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("actions version must be 1")
	}
	if len(c.Actions) == 0 || len(c.Actions) > maxActions {
		return fmt.Errorf("actions must contain 1–%d recipes", maxActions)
	}
	for id, recipe := range c.Actions {
		if !namePattern.MatchString(id) {
			return fmt.Errorf("invalid action ID %q", id)
		}
		if err := recipe.validate(); err != nil {
			return fmt.Errorf("action %q: %w", id, err)
		}
	}
	return nil
}

func (r Recipe) validate() error {
	if strings.TrimSpace(r.Label) == "" || len(r.Scope) == 0 {
		return fmt.Errorf("label and scope are required")
	}
	for _, scope := range r.Scope {
		if !slices.Contains([]string{"pane", "window", "session"}, scope) {
			return fmt.Errorf("unknown scope %q", scope)
		}
	}
	if len(r.Steps) == 0 || len(r.Steps) > maxSteps {
		return fmt.Errorf("expected 1–%d steps", maxSteps)
	}
	names := make(map[string]bool)
	for i, step := range r.Steps {
		if err := step.validate(i == len(r.Steps)-1); err != nil {
			return fmt.Errorf("step %d: %w", i+1, err)
		}
		if step.Select != "" && names[step.Select] {
			return fmt.Errorf("duplicate selection %q", step.Select)
		}
		names[step.Select] = true
	}
	return nil
}

func (s Step) validate(last bool) error {
	if last {
		if s.Run == nil || s.Select != "" || s.Source != nil || s.Title != "" {
			return fmt.Errorf("last step must contain only run")
		}
		return s.Run.validate()
	}
	if s.Run != nil || !namePattern.MatchString(s.Select) || s.Source == nil {
		return fmt.Errorf("selection step requires a name and source")
	}
	return s.Source.validate()
}

func (s SourceSpec) validate() error {
	variants := 0
	if s.Builtin != "" {
		variants++
		if !slices.Contains([]string{"tmux.sessions", "tmux.windows", "tmux.panes"}, s.Builtin) {
			return fmt.Errorf("unknown source %q", s.Builtin)
		}
	}
	if s.Exec != nil {
		variants++
		if err := s.Exec.validate(); err != nil {
			return err
		}
	}
	if s.Items != nil {
		variants++
		if err := validateFixedItems(s.Items); err != nil {
			return err
		}
	}
	if variants != 1 {
		return fmt.Errorf("source requires exactly one of builtin, exec or items")
	}
	if s.Builtin == "" && (s.Session != "" || s.Window != "") {
		return fmt.Errorf("session/window filters require a builtin source")
	}
	if s.Builtin == "tmux.sessions" && s.Window != "" {
		return fmt.Errorf("window filter is not supported by tmux.sessions")
	}
	return nil
}

func (c Command) validate() error {
	if len(c.Argv) == 0 || len(c.Argv) > 128 || strings.TrimSpace(c.Argv[0]) == "" {
		return fmt.Errorf("command requires 1–128 arguments")
	}
	_, err := commandTimeout(c.Timeout)
	return err
}

func validateFixedItems(items []Item) error {
	if err := validateItems(items); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := json.Marshal(item.Value); err != nil {
			return fmt.Errorf("item %q value must be JSON-compatible: %w", item.ID, err)
		}
	}
	return nil
}

func validateItems(items []Item) error {
	if len(items) > MaxItems {
		return fmt.Errorf("source exceeds %d items", MaxItems)
	}
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		if err := validateItem(item); err != nil {
			return err
		}
		if ids[item.ID] {
			return fmt.Errorf("duplicate item ID %q", item.ID)
		}
		ids[item.ID] = true
	}
	return nil
}

func validateItem(item Item) error {
	if item.ID == "" || len(item.ID) > 256 || strings.TrimSpace(item.Label) == "" {
		return fmt.Errorf("items require a nonempty ID (at most 256 bytes) and label")
	}
	return nil
}
