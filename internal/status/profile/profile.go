// Package profile loads bounded, data-only application status profiles.
package profile

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/MuhametSmaili/hetki/internal/status/screen"
	"gopkg.in/yaml.v3"
)

const MaxProfileBytes = 32 << 10

type Source string

const (
	Protocol Source = "protocol"
	Screen   Source = "screen"
)

// Profile is a value copy of validated configuration. Screen is immutable and
// non-nil only for screen profiles. Generation fingerprints the loaded document.
type Profile struct {
	ID           string
	Command      string
	Generation   string
	Source       Source
	Screen       *screen.Evaluator
	PollInterval time.Duration
}

type document struct {
	Version int    `yaml:"version"`
	ID      string `yaml:"id"`
	Match   struct {
		Command string `yaml:"command"`
	} `yaml:"match"`
	Source sourceSpec `yaml:"source"`
}

type sourceSpec struct {
	Type     string      `yaml:"type"`
	Protocol *string     `yaml:"protocol"`
	Scope    *string     `yaml:"scope"`
	Screen   *screenSpec `yaml:"screen"`
}

type screenSpec struct {
	PollInterval string `yaml:"poll_interval"`
	Region       struct {
		LastNonblankRows int `yaml:"last_nonblank_rows"`
	} `yaml:"region"`
	Rules []ruleSpec `yaml:"rules"`
}

type ruleSpec struct {
	State string        `yaml:"state"`
	All   []matcherSpec `yaml:"all"`
	Any   []matcherSpec `yaml:"any"`
	None  []matcherSpec `yaml:"none"`
}

type matcherSpec struct {
	Contains string `yaml:"contains"`
	Regex    string `yaml:"regex"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

// Load accepts exactly one strict version-2 YAML document. The reader must be
// finite or cancellation-aware; at most MaxProfileBytes+1 bytes are read.
func Load(reader io.Reader) (Profile, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxProfileBytes+1))
	if err != nil {
		return Profile{}, fmt.Errorf("read profile: %w", err)
	}
	if len(data) > MaxProfileBytes {
		return Profile{}, fmt.Errorf("profile exceeds %d bytes", MaxProfileBytes)
	}

	var doc document
	if err := decode(data, &doc); err != nil {
		return Profile{}, err
	}
	if doc.Version != 2 || !identifier.MatchString(doc.ID) || !identifier.MatchString(doc.Match.Command) {
		return Profile{}, fmt.Errorf("expected version 2 and bounded id/command identifiers")
	}

	p := Profile{ID: doc.ID, Command: doc.Match.Command, Generation: fmt.Sprintf("%x", sha256.Sum256(data))}
	if err := p.compileSource(doc.Source); err != nil {
		return Profile{}, fmt.Errorf("profile %s: %w", doc.ID, err)
	}
	return p, nil
}

func decode(data []byte, doc *document) error {
	var tree yaml.Node
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return fmt.Errorf("decode profile: %w", err)
	}
	if err := validateYAML(&tree, 0); err != nil {
		return err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(doc); err != nil {
		return fmt.Errorf("decode profile fields: %w", err)
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("profile requires exactly one YAML document")
	}
	return nil
}

func validateYAML(node *yaml.Node, depth int) error {
	if depth > 16 || node.Kind == yaml.AliasNode || node.Tag == "!!merge" || node.Tag == "!!null" {
		return fmt.Errorf("profile YAML cannot contain aliases, merges, nulls or nesting beyond 16 levels")
	}
	for _, child := range node.Content {
		if err := validateYAML(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (p *Profile) compileSource(spec sourceSpec) error {
	switch spec.Type {
	case "protocol":
		if spec.Protocol != nil || spec.Scope != nil || spec.Screen != nil {
			return fmt.Errorf("protocol source takes no additional settings")
		}
		p.Source = Protocol
	case "osc": // Preserve existing version-2 protocol profiles at the loading boundary.
		if spec.Protocol == nil || *spec.Protocol != "osc7501" || spec.Scope == nil || *spec.Scope != "root" || spec.Screen != nil {
			return fmt.Errorf("osc source requires protocol osc7501, scope root and no screen settings")
		}
		p.Source = Protocol
	case "screen":
		if spec.Screen == nil || spec.Protocol != nil || spec.Scope != nil {
			return fmt.Errorf("screen source requires screen settings and no protocol/scope")
		}
		return p.compileScreen(*spec.Screen)
	default:
		return fmt.Errorf("unsupported source %q", spec.Type)
	}
	return nil
}

func (p *Profile) compileScreen(spec screenSpec) error {
	interval, err := time.ParseDuration(spec.PollInterval)
	if err != nil || interval < time.Second || interval > time.Minute {
		return fmt.Errorf("poll_interval must be between 1s and 1m")
	}

	rules := make([]screen.Rule, len(spec.Rules))
	for i, rule := range spec.Rules {
		state := status.State(rule.State)
		if rule.State == "needs-input" {
			state = status.Blocked
		}
		rules[i] = screen.Rule{State: state, All: matchers(rule.All), Any: matchers(rule.Any), None: matchers(rule.None)}
	}
	evaluator, err := screen.Compile(screen.Config{LastNonblankRows: spec.Region.LastNonblankRows, Rules: rules})
	if err != nil {
		return err
	}
	p.Source, p.Screen, p.PollInterval = Screen, evaluator, interval
	return nil
}

func matchers(specs []matcherSpec) []screen.Matcher {
	result := make([]screen.Matcher, len(specs))
	for i, spec := range specs {
		result[i] = screen.Matcher{Contains: spec.Contains, Regex: spec.Regex}
	}
	return result
}
