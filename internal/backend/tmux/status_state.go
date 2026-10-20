package tmux

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
)

// QueryStatusState gets the picker rows and the server details in one tmux
// request. Thus both describe the same server.
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

// stateQueryRunner adds the picker row query to the request from ResolveServer.
// It keeps the rows and gives only the server details back to ResolveServer.
// Use it for one ResolveServer call only.
type stateQueryRunner struct {
	client Client
	query  LoadStateQuery
	rows   string
}

func (r *stateQueryRunner) RunLimited(ctx context.Context, limit int, args ...string) (string, error) {
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
