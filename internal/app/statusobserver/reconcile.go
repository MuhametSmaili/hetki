package statusobserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
)

// Reconcile revokes stale routes before doing I/O, then opens missing session
// subscriptions sequentially under one two-second budget. Existing valid owners
// are retained even if a lexically earlier linked session appears. Partial
// success remains usable; failures are returned for the coordinator to diagnose
// and retry with a later inventory. Inventory native problems disable routing.
// A failed session's retry deadline also applies to manual reconciliation calls.
func (s *Streams) Reconcile(ctx context.Context, inventory observe.Inventory) error {
	return s.reconcile(ctx, collectTopology(inventory))
}

type paneFacts struct {
	pane     observe.Pane
	sessions []string
	valid    bool
}

type topology struct {
	server   string
	revision uint64
	panes    map[string]paneFacts
}

func collectTopology(inventory observe.Inventory) topology {
	view := topology{
		server:   inventory.Server(),
		revision: inventory.Revision(),
		panes:    make(map[string]paneFacts),
	}
	for id, pane := range inventory.All() {
		view.panes[id] = paneFacts{pane: pane, valid: inventory.Problem(id) == nil}
	}

	for location := range inventory.Locations() {
		facts := view.panes[location.Pane]
		facts.sessions = append(facts.sessions, location.Session)
		view.panes[location.Pane] = facts
	}
	return view
}

func (s *Streams) reconcile(ctx context.Context, inventory topology) error {
	s.operations.Lock()
	defer s.operations.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()

	selected, retired, err := s.prepare(ctx, inventory)
	if err != nil {
		return err
	}

	var result error
	for _, sub := range retired {
		result = errors.Join(result, stopSubscription(sub))
	}

	for _, session := range selected {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		result = errors.Join(result, s.start(ctx, session))
	}

	if len(selected) == maxSubscriptions && sessionCount(inventory) > maxSubscriptions {
		result = errors.Join(result, ErrSubscriptionLimit)
	}
	return result
}

func (s *Streams) prepare(ctx context.Context, inventory topology) ([]string, []*subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, nil, ErrClosed
	}

	if err := s.ctx.Err(); err != nil {
		return nil, nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if inventory.revision <= s.revision {
		return nil, nil, ErrStaleInventory
	}

	if inventory.server != s.server {
		return nil, nil, observe.ErrServerChanged
	}

	s.revision = inventory.revision
	selected := s.selectSessions(inventory)
	retired := s.retire(selected)
	s.updateRoutes(inventory)
	return selected, retired, nil
}

func sessionCount(inventory topology) int {
	sessions := make(map[string]bool)
	for _, facts := range inventory.panes {
		for _, session := range facts.sessions {
			sessions[session] = true
		}
	}
	return len(sessions)
}

func (s *Streams) selectSessions(inventory topology) []string {
	seen := make(map[string]bool)
	for _, facts := range inventory.panes {
		for _, session := range facts.sessions {
			seen[session] = true
		}
	}
	sessions := make([]string, 0, len(seen))
	for session := range seen {
		sessions = append(sessions, session)
	}
	slices.Sort(sessions)

	// Preserve existing subscriptions at capacity, avoiding needless handoffs.
	slices.SortStableFunc(sessions, func(a, b string) int {
		return activeOrder(s.subscriptions[a]) - activeOrder(s.subscriptions[b])
	})
	return sessions[:min(len(sessions), maxSubscriptions)]
}

func activeOrder(sub *subscription) int {
	if sub != nil && sub.active {
		return 0
	}
	return 1
}

func (s *Streams) retire(selected []string) []*subscription {
	for session := range s.retries {
		if !slices.Contains(selected, session) {
			delete(s.retries, session)
		}
	}

	var retired []*subscription
	for session, sub := range s.subscriptions {
		if !sub.active || !slices.Contains(selected, session) {
			sub.active = false
			delete(s.subscriptions, session)
			retired = append(retired, sub)
		}
	}
	return retired
}

func (s *Streams) updateRoutes(inventory topology) {
	for id := range s.panes {
		if _, found := inventory.panes[id]; !found {
			s.consumer.Reset(Scope{Pane: id})
			delete(s.panes, id)
		}
	}

	for _, facts := range inventory.panes {
		s.updateRoute(facts.pane, facts.sessions, facts.valid)
	}
}

func (s *Streams) updateRoute(facts observe.Pane, sessions []string, valid bool) {
	pane := s.panes[facts.ID]
	if pane == nil {
		pane = &route{}
		s.panes[facts.ID] = pane
	}

	changed := !sameLifetime(pane.pane, facts)
	pane.pane = facts
	slices.Sort(sessions)
	pane.sessions = slices.Compact(sessions)
	pane.available = valid && !facts.Dead && facts.Root.Identity() != "" && facts.Foreground.Identity() != ""
	owner := s.owner(pane)
	if changed || pane.owner != owner || pane.scope.Generation == 0 {
		s.assign(pane, owner)
	}
}

func sameLifetime(a, b observe.Pane) bool {
	return a.ID == b.ID && a.RootPID == b.RootPID && a.Dead == b.Dead &&
		a.TTY == b.TTY && a.Command == b.Command && a.Root == b.Root && a.Foreground == b.Foreground
}

func (s *Streams) owner(pane *route) *subscription {
	if !pane.available {
		return nil
	}

	if pane.owner != nil && pane.owner.active && slices.Contains(pane.sessions, pane.owner.session) {
		return pane.owner
	}

	for _, session := range pane.sessions {
		if sub := s.subscriptions[session]; sub != nil && sub.active {
			return sub
		}
	}
	return nil
}

func (s *Streams) assign(pane *route, owner *subscription) {
	s.generation++
	pane.owner = owner
	pane.scope = Scope{
		Pane:       pane.pane.ID,
		Root:       pane.pane.Root,
		Foreground: pane.pane.Foreground,
		Generation: s.generation,
	}
	if owner != nil {
		pane.scope.Connection = owner.connection.Generation()
	}
	s.consumer.Reset(pane.scope)
}

func (s *Streams) start(ctx context.Context, session string) error {
	s.mu.Lock()
	existing := s.subscriptions[session]
	retry := s.retries[session]
	s.mu.Unlock()
	if existing != nil || time.Now().Before(retry.after) {
		return nil
	}

	conn, err := s.open(ctx, session)
	if err != nil {
		if s.ctx.Err() == nil {
			s.mu.Lock()
			s.retryLater(session, false)
			s.mu.Unlock()
		}
		return fmt.Errorf("subscribe to tmux session %s: %w", session, err)
	}

	if err := ctx.Err(); err != nil {
		return errors.Join(err, conn.Close())
	}

	sub := &subscription{
		session:    session,
		connection: conn,
		done:       make(chan struct{}),
		active:     true,
		started:    time.Now(),
	}

	s.mu.Lock()
	s.subscriptions[session] = sub
	for _, pane := range s.panes {
		if owner := s.owner(pane); owner != pane.owner {
			s.assign(pane, owner)
		}
	}
	s.mu.Unlock()
	go s.read(sub)
	return nil
}
