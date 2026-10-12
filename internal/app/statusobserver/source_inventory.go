package statusobserver

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/service"
)

// Validation includes inventory, connection startup and a possible capture ahead
// of the next recovery pass. It is never extended by output or a stream reset.
const sourceValidation = 10 * time.Second

type sourcePane struct {
	facts     observe.Pane
	root      observe.Process
	valid     bool
	selection Selection
	stream    Scope
	pane      service.Pane
	protocol  *protocolBinding
}

func (s *Source) prepare(inventory observe.Inventory) error {
	selected := make(map[string]Selection)
	for id, pane := range inventory.All() {
		if s.selectProfile != nil {
			selection := s.selectProfile(pane)
			if err := validateSelection(selection); err != nil {
				return fmt.Errorf("select pane %s profile: %w", id, err)
			}

			selected[id] = selection
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if inventory.Server() != s.server {
		return observe.ErrServerChanged
	}

	if inventory.Revision() <= s.nativeRevision {
		return ErrStaleInventory
	}

	s.expireProtocols(time.Now())

	rows := make(map[string]sourcePane)
	panes := make([]service.Pane, 0)
	for id, facts := range inventory.All() {
		row := s.reconcilePane(s.panes[id], facts, selected[id], inventory.Problem(id) == nil)
		if row.valid {
			row.protocol.resolvePrompt(inventory.StartedAt(), facts.Foreground == facts.Root)
		}
		rows[id] = row
		panes = append(panes, row.pane)
	}

	s.revision++
	if err := s.core.UpdateInventory(service.Inventory{
		Revision:   s.revision,
		ValidUntil: inventory.StartedAt().Add(sourceValidation),
		Panes:      panes,
	}); err != nil {
		return err
	}

	s.panes = rows
	s.locations = slices.Collect(inventory.Locations())
	s.validatedUntil = inventory.StartedAt().Add(sourceValidation)
	if err := s.bindProtocols(); err != nil {
		return err
	}

	s.nativeRevision = inventory.Revision()
	return nil
}

func validateSelection(selection Selection) error {
	if selection.Profile.Source == service.Screen {
		if selection.Evaluate == nil {
			return fmt.Errorf("screen profile requires an evaluator")
		}
		return validateScreenInterval(selection.PollInterval)
	}
	if selection.Evaluate != nil || selection.Region != (observe.Region{}) || selection.PollInterval != 0 {
		return fmt.Errorf("screen evaluator, region or interval requires screen source selection")
	}
	return nil
}

func (s *Source) reconcilePane(previous sourcePane, facts observe.Pane, selected Selection, valid bool) sourcePane {
	ref := previous.pane.Ref
	if newPaneLife(previous, facts) {
		s.generation++
		ref = status.PaneRef{
			Server:     s.server,
			ID:         facts.ID,
			Generation: s.epoch + ":" + strconv.FormatUint(s.generation, 10),
		}
	}

	// Missing native facts cannot establish that this pane's profile stopped matching.
	if !valid && ref == previous.pane.Ref {
		selected = previous.selection
	}

	row := sourcePane{
		facts:     facts,
		root:      previous.root,
		valid:     valid,
		selection: selected,
		stream:    previous.stream,
		pane: service.Pane{
			Ref:      ref,
			Profile:  selected.Profile,
			Geometry: service.Geometry{Columns: facts.Columns, Rows: facts.Rows},
		},
	}
	if facts.Root.Identity() != "" {
		row.root = facts.Root
	}

	row.pane.Scope = row.serviceScope()
	row.protocol = reconcileProtocol(previous, row)
	return row
}

func newPaneLife(previous sourcePane, facts observe.Pane) bool {
	if previous.pane.Ref.ID == "" || previous.facts.Dead && !facts.Dead {
		return true
	}

	if previous.facts.RootPID > 0 && facts.RootPID > 0 && previous.facts.RootPID != facts.RootPID {
		return true
	}

	return facts.Root.Identity() != "" && previous.root.Identity() != "" && facts.Root != previous.root
}

func (p sourcePane) serviceScope() service.Scope {
	scope := service.Scope{
		Root:       p.facts.Root.Identity(),
		Foreground: p.facts.Foreground.Identity(),
	}
	if p.valid && !p.facts.Dead && p.stream.Connection != "" && p.stream.Root == p.facts.Root && p.stream.Foreground == p.facts.Foreground {
		scope.Connection = p.stream.Connection + ":" + strconv.FormatUint(p.stream.Generation, 10)
	}
	return scope
}

func (s *Source) publish(inventory observe.Inventory, acquisitionErr error) error {
	s.mu.Lock()
	err := s.publishLocked(inventory)
	s.mu.Unlock()

	if acquisitionErr != nil || err != nil {
		s.report(errors.Join(acquisitionErr, err))
	}

	if errors.Is(err, service.ErrClosed) || errors.Is(err, service.ErrStale) {
		return err
	}
	return nil
}

func (s *Source) publishLocked(inventory observe.Inventory) error {
	if inventory.Revision() == 0 {
		s.validatedUntil = time.Time{}
		for _, row := range s.panes {
			if row.protocol != nil {
				row.protocol.reset()
			}
		}
		s.revision++
		return s.core.Invalidate(s.revision)
	}

	plans := make([]ScreenPlan, 0)
	for _, row := range s.panes {
		if row.selection.Profile.Source == service.Screen {
			plans = append(plans, ScreenPlan{
				Pane:         row.pane.Ref,
				Region:       row.selection.Region,
				Evaluate:     row.selection.Evaluate,
				PollInterval: row.selection.PollInterval,
			})
		}
	}
	return s.screens.Update(inventory, plans)
}
