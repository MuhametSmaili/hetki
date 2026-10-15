package tmux

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
)

// QueryStatusState collects rows inside native server identity validation. The
// second metadata query also carries the row query, avoiding a separate discovery
// pass on each side of loading the picker.
func (b *TmuxBackend) QueryStatusState(ctx context.Context) (backend.StateResult, observe.Server, error) {
	query := LoadStateQuery{IncludeClients: isInsideTmux(), IncludeProgramInfo: true}
	runner := stateQueryRunner{client: b.client, query: query}
	server, err := observe.ResolveServer(ctx, &runner)
	if err != nil {
		return backend.StateResult{}, observe.Server{}, err
	}
	parsed, err := query.Parse(runner.rows)
	if err != nil {
		return backend.StateResult{}, observe.Server{}, err
	}
	state, err := projectLiveState(ctx, parsed, nil)
	return state, server, err
}

// ResolveServer samples metadata, reads native birth, then repeats both checks.
// Place row collection in its second request so native birth brackets the rows.
// The runner belongs to one synchronous ResolveServer call and retains no rows
// after QueryStatusState returns.
type stateQueryRunner struct {
	client Client
	query  LoadStateQuery
	primed bool
	rows   string
}

func (r *stateQueryRunner) RunLimited(ctx context.Context, limit int, args ...string) (string, error) {
	if !r.primed {
		r.primed = true
		return r.client.RunLimited(ctx, limit, args...)
	}
	combined := append(slices.Clone(args), ";")
	combined = append(combined, r.query.Args()...)
	// Large picker inventories remain independent of the daemon's pane ceiling.
	output, err := r.client.RunLimited(ctx, 16<<20, combined...)
	if err != nil {
		return "", err
	}
	metadata, rows, found := strings.Cut(output, "\n")
	if !found || len(metadata)+1 > limit {
		return "", fmt.Errorf("invalid combined tmux state metadata")
	}
	r.rows = rows
	return metadata + "\n", nil
}
