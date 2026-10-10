package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/MSmaili/hetki/internal/status"
)

const maxIdentityBytes = 256

type Source string

const (
	OSC7501 Source = "osc7501"
	Screen  Source = "screen"
)

// Scope fields are incarnation tokens, not PIDs or reusable command names.
// An empty Command is allowed before shell lifecycle tracking is available.
type Scope struct {
	Root       string
	Foreground string
	Connection string
	Command    string
}

// Profile selects an evidence source; its zero value selects generic OSC7501.
// Generation changes when matching or evaluation configuration changes, even if
// the profile name stays the same.
type Profile struct {
	Source     Source
	Generation string
}

type Geometry struct {
	Columns int
	Rows    int
}

type Pane struct {
	Ref      status.PaneRef
	Scope    Scope
	Profile  Profile
	Geometry Geometry
}

// Inventory is complete for this server. Revision is assigned before acquisition
// and increases across updates and invalidations, so late work cannot undo them.
// It orders collection, not UI changes. ValidUntil bounds native validation age.
type Inventory struct {
	Revision   uint64
	ValidUntil time.Time
	Panes      []Pane
}

func (s *Service) UpdateInventory(inventory Inventory) error {
	if err := validateInventory(s.server, inventory); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}

	now := time.Now()
	if inventory.Revision <= s.revision || !now.Before(inventory.ValidUntil) {
		return ErrStale
	}

	continuous := now.Before(s.validUntil)
	panes := make(map[string]record, len(inventory.Panes))
	for _, pane := range inventory.Panes {
		previous := s.panes[pane.Ref.ID]
		row := s.reconcile(previous, pane, continuous)
		panes[row.pane.Ref.ID] = row
	}

	s.panes = panes
	s.revision = inventory.Revision
	s.validUntil = inventory.ValidUntil
	s.changed()
	return nil
}

func (s *Service) reconcile(previous record, pane Pane, continuous bool) record {
	if previous.pane == pane && continuous {
		return previous
	}

	row := previous
	row.pane = clonePane(pane)
	if continuous && sameEvidenceScope(previous.pane, pane) {
		return row
	}

	s.generation++
	row.generation = s.generation
	row.sequence = 0
	row.value = status.Value{}
	if pane.Profile.Source != "" || previous.pane.Ref == pane.Ref && previous.value.State != status.Unreported {
		row.value.State = status.Unknown
	}

	if sameEvidenceScope(previous.pane, pane) && previous.value.State == status.Withdrawn {
		row.value.State = status.Withdrawn
	}

	return row
}

func sameEvidenceScope(previous, next Pane) bool {
	if previous.Ref != next.Ref || previous.Scope != next.Scope || previous.Profile != next.Profile {
		return false
	}

	return next.source() != Screen || previous.Geometry == next.Geometry
}

// Invalidate revokes outstanding targets immediately without retiring panes or
// releasing a source claim. A newer inventory and fresh evidence are required.
func (s *Service) Invalidate(revision uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}

	if revision <= s.revision {
		return ErrStale
	}

	for id, row := range s.panes {
		s.generation++
		row.generation = s.generation
		row.sequence = 0
		if row.value.State != status.Unreported && row.value.State != status.Withdrawn {
			row.value = status.Value{State: status.Unknown}
		}
		s.panes[id] = row
	}

	s.revision = revision
	s.validUntil = time.Time{}
	s.changed()
	return nil
}

func (p Pane) source() Source {
	if p.Profile.Source == "" {
		return OSC7501
	}

	return p.Profile.Source
}

func (p Pane) available() bool {
	if p.Scope.Root == "" || p.Scope.Foreground == "" || p.Scope.Connection == "" {
		return false
	}

	return p.source() != Screen || p.Geometry.Columns > 0 && p.Geometry.Rows > 0
}

// Small identity substrings must not retain a whole inventory response buffer.
func clonePane(pane Pane) Pane {
	pane.Ref.Server = strings.Clone(pane.Ref.Server)
	pane.Ref.ID = strings.Clone(pane.Ref.ID)
	pane.Ref.Generation = strings.Clone(pane.Ref.Generation)
	pane.Scope.Root = strings.Clone(pane.Scope.Root)
	pane.Scope.Foreground = strings.Clone(pane.Scope.Foreground)
	pane.Scope.Connection = strings.Clone(pane.Scope.Connection)
	pane.Scope.Command = strings.Clone(pane.Scope.Command)
	pane.Profile.Generation = strings.Clone(pane.Profile.Generation)
	pane.Profile.Source = Source(strings.Clone(string(pane.Profile.Source)))
	return pane
}

func validateInventory(server string, inventory Inventory) error {
	if inventory.Revision == 0 || len(inventory.Panes) > status.MaxPanes {
		return fmt.Errorf("status inventory requires a revision and at most %d panes", status.MaxPanes)
	}

	seen := make(map[string]struct{}, len(inventory.Panes))
	for _, pane := range inventory.Panes {
		if err := validatePane(server, pane); err != nil {
			return err
		}

		if _, duplicate := seen[pane.Ref.ID]; duplicate {
			return fmt.Errorf("status inventory contains duplicate pane IDs")
		}
		seen[pane.Ref.ID] = struct{}{}
	}

	return nil
}

func validatePane(server string, pane Pane) error {
	if pane.Ref.Server != server || pane.Ref.ID == "" || pane.Ref.Generation == "" {
		return fmt.Errorf("status pane requires matching server, ID and generation")
	}

	identities := []string{
		pane.Ref.ID, pane.Ref.Generation,
		pane.Scope.Root, pane.Scope.Foreground, pane.Scope.Connection, pane.Scope.Command,
		pane.Profile.Generation,
	}
	for _, identity := range identities {
		if len(identity) > maxIdentityBytes {
			return fmt.Errorf("status identity exceeds %d bytes", maxIdentityBytes)
		}
	}

	if pane.Geometry.Columns < 0 || pane.Geometry.Rows < 0 {
		return fmt.Errorf("negative status pane geometry")
	}

	switch pane.Profile.Source {
	case "":
		if pane.Profile.Generation != "" {
			return fmt.Errorf("status profile generation requires a source")
		}
	case OSC7501, Screen:
		if pane.Profile.Generation == "" {
			return fmt.Errorf("status profile requires a generation")
		}
	default:
		return fmt.Errorf("unsupported status profile source")
	}

	return nil
}
