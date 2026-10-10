package statusobserve

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/service"
)

const (
	screenBatchSize   = 128
	screenSettle      = 50 * time.Millisecond
	screenMaxDirtyAge = 250 * time.Millisecond
	screenBatchGap    = 100 * time.Millisecond
	screenRecovery    = 2 * time.Second
	screenFreshness   = 5 * time.Second
)

// ScreenPlan binds an immutable evaluator and region to a selected service pane.
// Composition must change the service profile generation whenever rules change.
// Evaluate must be bounded and must not retain text; it performs no acquisition.
type ScreenPlan struct {
	Pane     status.PaneRef
	Region   observe.Region
	Evaluate func(string) (status.State, error)
}

type screenEntry struct {
	plan       ScreenPlan
	native     observe.CaptureTarget
	target     service.Target
	firstDirty time.Time
	lastDirty  time.Time
	recovery   time.Time
	retryAt    time.Time
	failures   uint8
}

// Screens coalesces selected panes' capture work. It owns no worker or timer;
// Watch runs its batches alongside inventory acquisition. Update and Dirty may
// run concurrently with the watch. Only this owner should apply screen evidence
// to its service; the service itself remains owned by application composition.
type Screens struct {
	mu        sync.Mutex
	service   *service.Service
	report    func(error)
	capture   func(context.Context, []observe.CaptureTarget) ([]observe.Screen, error)
	entries   map[string]*screenEntry
	revision  uint64
	sequence  uint64
	notBefore time.Time
	wake      chan struct{}
	watching  atomic.Bool
}

// NewScreens creates a screen-work owner. report receives capture/evaluation
// failures from the watch outside locks and must remain bounded. Each accepted
// activity expires five seconds after capture start, independently of inventory.
func NewScreens(sampler *observe.Sampler, core *service.Service, report func(error)) (*Screens, error) {
	if sampler == nil || core == nil || report == nil {
		return nil, fmt.Errorf("screen scheduling requires sampler, status service and error reporter")
	}
	screens := newScreens(core, sampler.Capture)
	screens.report = report
	return screens, nil
}

func newScreens(core *service.Service, capture func(context.Context, []observe.CaptureTarget) ([]observe.Screen, error)) *Screens {
	return &Screens{
		service: core,
		capture: capture,
		entries: make(map[string]*screenEntry),
		wake:    make(chan struct{}, 1),
	}
}

// Update replaces the complete selected screen set after composition updates the
// service inventory. Unavailable targets are omitted. Per-pane acquisition errors
// are returned while other valid plans remain scheduled. Invalid plans or stale
// inventories reject the whole update. Revisions are native acquisition order.
func (s *Screens) Update(inventory observe.Inventory, plans []ScreenPlan) error {
	if err := validateScreenPlans(inventory, plans); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if inventory.Revision() <= s.revision {
		return ErrStaleInventory
	}

	entries := make(map[string]*screenEntry, len(plans))
	var result error
	for _, plan := range plans {
		target, ok := s.service.Target(plan.Pane, service.Screen)
		if !ok {
			continue
		}
		plan.Pane = target.Pane.Ref

		native, err := inventory.Target(plan.Pane.ID, plan.Region)
		if err == nil {
			err = matchScreenScope(target, native)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("select screen %s: %w", plan.Pane.ID, err))
			result = errors.Join(result, s.unknown(target))
			continue
		}
		entries[plan.Pane.ID] = &screenEntry{plan: plan, native: native, target: target}
	}
	return errors.Join(result, s.replace(inventory.Revision(), entries))
}

func matchScreenScope(target service.Target, native observe.CaptureTarget) error {
	facts := native.Pane()
	pane := target.Pane
	if pane.Scope.Root != facts.Root.Identity() || pane.Scope.Foreground != facts.Foreground.Identity() ||
		pane.Geometry.Columns != facts.Columns || pane.Geometry.Rows != facts.Rows {
		return fmt.Errorf("%w: native screen facts differ from service scope", observe.ErrChanged)
	}
	return nil
}

func validateScreenPlans(inventory observe.Inventory, plans []ScreenPlan) error {
	if inventory.Revision() == 0 || len(plans) > status.MaxPanes {
		return fmt.Errorf("screen scheduling requires a revision and at most %d panes", status.MaxPanes)
	}
	seen := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if plan.Pane.Server != inventory.Server() || plan.Pane.ID == "" || plan.Pane.Generation == "" || plan.Evaluate == nil {
			return fmt.Errorf("screen plan requires a matching pane and evaluator")
		}
		if seen[plan.Pane.ID] {
			return fmt.Errorf("duplicate screen plan %s", plan.Pane.ID)
		}
		seen[plan.Pane.ID] = true
	}
	return nil
}

// Caller holds mu. Identical refreshes retain dirty/recovery deadlines and
// in-flight identity. Replaced plans cannot receive an earlier batch's result.
func (s *Screens) replace(revision uint64, entries map[string]*screenEntry) error {
	now := time.Now()
	for id, next := range entries {
		previous := s.entries[id]
		if previous != nil && previous.target == next.target && previous.plan.Region == next.plan.Region && previous.native.Pane() == next.native.Pane() {
			previous.native = next.native
			entries[id] = previous
		} else {
			next.firstDirty, next.lastDirty = now, now
		}
	}
	var result error
	for id, previous := range s.entries {
		if entries[id] != previous {
			result = errors.Join(result, s.unknown(previous.target))
		}
	}
	s.entries = entries
	s.revision = revision
	s.signal()
	return result
}

func (s *Screens) unknown(target service.Target) error {
	s.sequence++
	err := s.service.Apply(target, s.sequence, status.Value{State: status.Unknown})
	if errors.Is(err, service.ErrStale) {
		return nil
	}
	return err
}

// Dirty records a redraw hint only for a selected pane. Repeated output changes
// the settling deadline but cannot postpone collection past the maximum dirty age.
func (s *Screens) Dirty(pane string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[pane]
	if entry == nil {
		return
	}
	entry.lastDirty = time.Now()
	if entry.firstDirty.IsZero() {
		entry.firstDirty = entry.lastDirty
		s.signal()
	}
}

func (s *Screens) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Screens) changes() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.wake
}

func (s *Screens) clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result error
	for _, entry := range s.entries {
		result = errors.Join(result, s.unknown(entry.target))
	}
	s.entries = make(map[string]*screenEntry)
	s.signal()
	return result
}
