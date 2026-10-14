package observe

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"time"
)

const (
	maxInventoryPanes = 1024
	maxLocations      = 8192
	maxInventoryBytes = 1 << 20
	acquisitionBudget = 2 * time.Second
)

// ErrChanged means facts no longer match the acquisition target. Fresh inventory
// is required; a failed capture does not establish an application state.
var ErrChanged = errors.New("tmux observation facts changed")

// Pane separates tmux pane facts from native root/foreground process lifetimes.
// Command is tmux's display hint, not proof of the identity of an output writer.
// A foreground process denotes the group leader, not every member of a pipeline.
type Pane struct {
	ID         string
	RootPID    int
	Dead       bool
	TTY        string
	Command    string
	Columns    int
	Rows       int
	Alternate  bool
	Root       Process
	Foreground Process
}

// Location is one session/window occurrence of a pane. Linked windows can give
// one pane several locations; All returns the pane only once.
type Location struct {
	Session string
	Window  string
	Pane    string
}

// Inventory is immutable. Revision is supplied before acquisition by the caller;
// it orders collection, not pane incarnations or user-visible changes.
type Inventory struct {
	server    string
	revision  uint64
	started   time.Time
	panes     map[string]Pane
	locations []Location
	problems  map[string]error
}

func (i Inventory) Revision() uint64 { return i.revision }

// Server identifies the exact server incarnation sampled by this inventory.
func (i Inventory) Server() string { return i.server }

func (i Inventory) StartedAt() time.Time { return i.started }

func (i Inventory) All() iter.Seq2[string, Pane] { return maps.All(i.panes) }

func (i Inventory) Locations() iter.Seq[Location] { return slices.Values(i.locations) }

func (i Inventory) Lookup(id string) (Pane, bool) {
	pane, found := i.panes[id]
	return pane, found
}

// Problem distinguishes unreadable or unstable facts from a confirmed dead pane.
func (i Inventory) Problem(id string) error { return i.problems[id] }

// Sampler performs synchronous bounded acquisitions. It retains no inventories,
// screens, timers or workers. It selects the supplied socket for every CLI query.
// Runner must support concurrent calls if its owner uses Inventory and Capture concurrently.
type Sampler struct {
	server  Server
	runner  Runner
	process func(context.Context, int) (processInfo, error)
}

// NewSampler pins acquisition to an already resolved server incarnation.
func NewSampler(server Server, runner Runner) (*Sampler, error) {
	if runner == nil {
		return nil, fmt.Errorf("tmux sampler requires a runner")
	}

	if err := server.validateDescriptor(); err != nil {
		return nil, err
	}

	if _, err := headerLayoutForVersion(server.Version); err != nil {
		return nil, err
	}

	return &Sampler{server: server, runner: runner, process: readProcess}, nil
}

// Inventory samples all pane occurrences twice. Unstable or unreadable native
// facts remain explicit per-pane problems; malformed or incomplete queries fail
// the whole acquisition. No previously sampled facts are reused.
func (s *Sampler) Inventory(ctx context.Context, revision uint64) (Inventory, error) {
	if revision == 0 {
		return Inventory{}, fmt.Errorf("inventory requires a nonzero revision")
	}

	ctx, cancel := context.WithTimeout(ctx, acquisitionBudget)
	defer cancel()
	started := time.Now()
	before, err := s.sample(ctx, nil)
	if err != nil {
		return Inventory{}, err
	}

	after, err := s.sample(ctx, nil)
	if err != nil {
		return Inventory{}, err
	}

	for id, pane := range after.panes {
		previous, found := before.panes[id]
		if !found || previous != pane || before.problems[id] != nil {
			after.problems[id] = errors.Join(ErrChanged, before.problems[id], after.problems[id])
			if previous.Root != pane.Root {
				pane.Root = Process{}
			}
			pane.Foreground = Process{}
			after.panes[id] = pane
		}
	}
	after.revision = revision
	after.started = started
	return after, ctx.Err()
}

func (s *Sampler) sample(ctx context.Context, ids []string) (Inventory, error) {
	processes := processSample{read: s.process, cache: make(map[int]processResult)}
	if err := s.validateProcess(ctx, &processes); err != nil {
		return Inventory{}, err
	}

	inventory, err := s.queryInventory(ctx, ids)
	if err != nil {
		return Inventory{}, err
	}
	return s.sampleProcesses(ctx, inventory, &processes)
}

// sampleProcesses adds fresh native facts to a tmux query. The caller validates
// the server before the query; the final validation bypasses the per-pass cache.
func (s *Sampler) sampleProcesses(ctx context.Context, inventory Inventory, processes *processSample) (Inventory, error) {
	for id, pane := range inventory.panes {
		if pane.Dead {
			continue
		}
		root, foreground, err := processes.paneProcesses(ctx, pane.RootPID)
		if err != nil {
			inventory.problems[id] = err
		}
		pane.Root, pane.Foreground = root, foreground
		inventory.panes[id] = pane
	}

	// The end check must bypass the per-pass cache to catch server exit/reuse.
	end := processSample{read: s.process, cache: make(map[int]processResult)}
	if err := s.validateProcess(ctx, &end); err != nil {
		return Inventory{}, err
	}
	return inventory, ctx.Err()
}

func (s *Sampler) validateProcess(ctx context.Context, processes *processSample) error {
	info, err := processes.lookup(ctx, s.server.PID)
	if err != nil {
		return fmt.Errorf("validate sampled tmux server: %w", err)
	}
	if info.Birth != s.server.Birth {
		return ErrServerChanged
	}
	return ctx.Err()
}
