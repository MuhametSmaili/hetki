package statusobserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/observe"
	"github.com/MSmaili/hetki/internal/status"
	"github.com/MSmaili/hetki/internal/status/service"
)

func (e *screenEntry) due() time.Time {
	if e.firstDirty.IsZero() {
		return later(e.recovery, e.retryAt)
	}
	dirty := earlier(e.lastDirty.Add(screenSettle), e.firstDirty.Add(screenMaxDirtyAge))
	return later(earlier(dirty, e.recovery), e.retryAt)
}

func (s *Screens) nextCapture() time.Time {
	if s == nil {
		return time.Time{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var next time.Time
	for _, entry := range s.entries {
		next = earlier(next, entry.due())
	}
	if !next.IsZero() && next.Before(s.notBefore) {
		return s.notBefore
	}
	return next
}

type screenJob struct {
	entry    *screenEntry
	native   observe.CaptureTarget
	sequence uint64
}

func (s *Screens) takeBatch() []screenJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Before(s.notBefore) {
		return nil
	}

	ready := make([]*screenEntry, 0, len(s.entries))
	for id, entry := range s.entries {
		current, ok := s.service.Target(entry.plan.Pane, service.Screen)
		if !ok || current != entry.target {
			delete(s.entries, id)
			continue
		}

		if !now.Before(entry.due()) {
			ready = append(ready, entry)
		}
	}

	slices.SortFunc(ready, compareScreenDue)
	jobs := make([]screenJob, 0, min(len(ready), screenBatchSize))
	for _, entry := range ready[:min(len(ready), screenBatchSize)] {
		s.sequence++
		jobs = append(jobs, screenJob{entry: entry, native: entry.native, sequence: s.sequence})
		entry.firstDirty, entry.lastDirty = time.Time{}, time.Time{}
		entry.recovery = now.Add(entry.plan.interval())
	}
	return jobs
}

func compareScreenDue(a, b *screenEntry) int {
	if order := a.due().Compare(b.due()); order != 0 {
		return order
	}
	return strings.Compare(a.plan.Pane.ID, b.plan.Pane.ID)
}

func (s *Screens) runBatch(ctx context.Context) error {
	jobs := s.takeBatch()
	if len(jobs) == 0 {
		return nil
	}

	targets := make([]observe.CaptureTarget, len(jobs))
	for i, job := range jobs {
		targets[i] = job.native
	}

	screens, captureErr := s.capture(ctx, targets)
	s.mu.Lock()
	s.notBefore = time.Now().Add(screenBatchGap)
	s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if captureErr == nil {
		captureErr = validateScreenBatch(jobs, screens)
	}

	result := captureErr
	for i, job := range jobs {
		var screen observe.Screen
		if captureErr == nil {
			screen = screens[i]
		}
		result = errors.Join(result, s.applyScreen(job, screen, captureErr))
	}
	return result
}

func validateScreenBatch(jobs []screenJob, screens []observe.Screen) error {
	if len(jobs) != len(screens) {
		return fmt.Errorf("incomplete screen batch")
	}
	for i, screen := range screens {
		if screen.Target != jobs[i].native {
			return fmt.Errorf("screen batch returned an unexpected target")
		}
	}
	return nil
}

func (s *Screens) applyScreen(job screenJob, screen observe.Screen, captureErr error) error {
	if !s.current(job) {
		return nil
	}

	value, err := evaluateScreen(job.entry.plan, screen, captureErr)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[job.entry.plan.Pane.ID] != job.entry {
		return nil
	}

	if err != nil || captureErr != nil {
		job.entry.failures = min(job.entry.failures+1, 3)
		job.entry.retryAt = time.Now().Add(retryDelay(job.entry.failures))
	} else {
		job.entry.failures = 0
		job.entry.retryAt = time.Time{}
	}

	applyErr := s.service.Apply(job.entry.target, job.sequence, value)
	if errors.Is(applyErr, service.ErrStale) {
		return nil
	}
	return errors.Join(err, applyErr)
}

func (s *Screens) current(job screenJob) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.service.Target(job.entry.plan.Pane, service.Screen)
	return ok && current == job.entry.target && s.entries[job.entry.plan.Pane.ID] == job.entry
}

func evaluateScreen(plan ScreenPlan, screen observe.Screen, captureErr error) (status.Value, error) {
	unknown := status.Value{State: status.Unknown}
	if captureErr != nil {
		return unknown, nil
	}

	if screen.Err != nil {
		return unknown, fmt.Errorf("capture pane %s: %w", plan.Pane.ID, screen.Err)
	}

	if screen.StartedAt.IsZero() || !time.Now().Before(screen.StartedAt.Add(plan.freshness())) {
		return unknown, fmt.Errorf("screen acquisition expired for pane %s", plan.Pane.ID)
	}

	state, err := plan.Evaluate(screen.Text)
	if err != nil {
		return unknown, fmt.Errorf("evaluate pane %s: %w", plan.Pane.ID, err)
	}

	if state == status.Unknown {
		return unknown, nil
	}

	switch state {
	case status.Working, status.Idle, status.Blocked:
		return status.Value{State: state, ValidUntil: screen.StartedAt.Add(plan.freshness())}, nil
	default:
		return unknown, fmt.Errorf("screen evaluator returned an unsupported state for pane %s", plan.Pane.ID)
	}
}
