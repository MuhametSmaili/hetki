package tmux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
)

// ActionState contains the socket path and server details.
// Socket can be known even when Server is nil.
// A non-nil, empty State.Invocation means the invocation is unknown.
type ActionState struct {
	State  backend.StateResult
	Socket string
	Server *observe.Server
}

// QueryActionState reads action context. It does not start a tmux server.
func (b *TmuxBackend) QueryActionState(ctx context.Context) (ActionState, error) {
	query := LoadStateQuery{
		IncludeClients:     isInsideTmux(),
		IncludeProgramInfo: true,
		NoStart:            true,
	}
	runner := stateQueryRunner{client: b.client, query: query}
	server, err := observe.ResolveServer(ctx, &runner)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ActionState{}, err
		}
		if absent, ok := errors.AsType[*absentServerError](err); ok {
			return ActionState{Socket: absent.socket, State: backend.StateResult{Invocation: actionInvocation(nil, nil)}}, nil
		}
		return ActionState{}, err
	}

	parsed, err := query.Parse(runner.rows)
	if err != nil {
		return ActionState{}, err
	}

	state, err := projectLiveState(ctx, parsed, nil)
	if err != nil {
		return ActionState{}, err
	}

	state.Invocation = actionInvocation(&server, parsed.clients)
	return ActionState{State: state, Socket: server.Socket, Server: &server}, nil
}

func actionInvocation(server *observe.Server, clients []clientState) *backend.Invocation {
	invocation := invocationContext()
	if invocation == nil {
		return nil
	}

	if server == nil || !invocationServerMatches(*server) || invocation.SessionID == "" {
		return &backend.Invocation{}
	}
	if invocation.PaneID != "" {
		return invocation
	}

	owner := invokingClient(clients)
	if owner.paneID == "" {
		return &backend.Invocation{}
	}
	return &backend.Invocation{SessionID: owner.sessionID, PaneID: owner.paneID}
}

func invocationServerMatches(server observe.Server) bool {
	// Socket paths can contain commas.
	value := os.Getenv("TMUX")
	end := strings.LastIndexByte(value, ',')
	if end < 0 {
		return false
	}

	value = value[:end]
	end = strings.LastIndexByte(value, ',')
	if end < 0 {
		return false
	}

	pid, err := strconv.Atoi(value[end+1:])
	if err != nil || pid != server.PID {
		return false
	}

	want, err := filepath.EvalSymlinks(value[:end])
	if err != nil {
		return false
	}

	actual, err := filepath.EvalSymlinks(server.Socket)
	return err == nil && want == actual
}
