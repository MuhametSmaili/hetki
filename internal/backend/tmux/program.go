package tmux

import (
	"context"
	"time"

	"github.com/MuhametSmaili/hetki/internal/foreground"
)

const (
	programBudget   = 10 * time.Millisecond
	maxProgramPanes = 32
)

// Optional display enrichment only: no subprocesses, polling, persistent cache,
// environment matching, or navigation/command semantics depend on this hint.
type programLookup struct {
	ctx       context.Context
	deadline  time.Time
	remaining int
	cache     map[int]string
}

func newProgramLookup(ctx context.Context) *programLookup {
	return &programLookup{ctx: ctx, deadline: time.Now().Add(programBudget), remaining: maxProgramPanes, cache: make(map[int]string)}
}

func (l *programLookup) resolve(pane Pane) string {
	// Restrict extra I/O to ambiguous Node observations, not known applications.
	if pane.Dead || pane.Command != "node" || pane.PID <= 0 {
		return ""
	}
	if name, found := l.cache[pane.PID]; found {
		return name
	}
	if l.ctx.Err() != nil || l.remaining == 0 || time.Now().After(l.deadline) {
		return ""
	}
	l.remaining--
	name := foreground.Name(l.ctx, pane.PID)
	if name == pane.Command {
		name = ""
	}
	l.cache[pane.PID] = name
	return name
}
