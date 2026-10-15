package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/internal/format"
)

const (
	queryTimeout    = 5 * time.Second
	mutationTimeout = 10 * time.Second
)

type TmuxBackend struct {
	client Client
}

func init() {
	backend.Register("tmux", func() (backend.Backend, error) {
		return NewBackend()
	})
}

func NewBackend() (*TmuxBackend, error) {
	c, err := New()
	if err != nil {
		return nil, err
	}
	return &TmuxBackend{client: c}, nil
}

func (b *TmuxBackend) Name() string {
	return "tmux"
}

func (b *TmuxBackend) QueryState(ctx context.Context) (backend.StateResult, error) {
	if err := ctx.Err(); err != nil {
		return backend.StateResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	result, err := RunQuery(ctx, b.client, LoadStateQuery{IncludeClients: isInsideTmux(), IncludeProgramInfo: true})
	return projectLiveState(ctx, result, err)
}

func projectLiveState(ctx context.Context, result LoadStateResult, err error) (backend.StateResult, error) {
	// tmux exits non-zero when list-panes runs against an empty server, after the
	// chained show-options calls have already emitted valid base indexes.
	var parseErr *queryParseError
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &parseErr) || len(result.Sessions) != 0 || !isEmptyServerError(err)) {
		return backend.StateResult{}, err
	}

	currentClient := invokingClient(result.clients)
	programs := newProgramLookup(ctx)
	sessions := make([]backend.Session, len(result.Sessions))
	for i, s := range result.Sessions {
		windows := make([]backend.Window, len(s.Windows))
		for j, w := range s.Windows {
			panes := make([]backend.Pane, len(w.Panes))
			for k, p := range w.Panes {
				panes[k] = backend.Pane{
					ID:      p.ID,
					Index:   p.Index,
					Path:    p.Path,
					Command: p.Command,
					Program: programs.resolve(p),
					Dead:    p.Dead,
					Zoom:    p.Zoom,
					Active:  p.Active,
				}
			}
			windows[j] = backend.Window{
				ID:     w.ID,
				Name:   w.Name,
				Index:  w.Index,
				Path:   w.Path,
				Layout: w.Layout,
				Active: w.Active,
				Panes:  panes,
			}
		}
		sessions[i] = backend.Session{
			ID:            s.ID,
			Name:          s.Name,
			WorkspacePath: s.WorkspacePath,
			DefaultShell:  s.DefaultShell,
			Last:          currentClient.lastSession != "" && s.Name == currentClient.lastSession && s.ID != currentClient.sessionID,
			Windows:       windows,
		}
	}

	return backend.StateResult{
		Sessions: sessions,
		Active: backend.ActiveContext{
			SessionID:   result.Active.SessionID,
			Session:     result.Active.Session,
			WindowID:    result.Active.WindowID,
			Window:      result.Active.Window,
			WindowIndex: result.Active.WindowIndex,
			PaneID:      result.Active.PaneID,
			Pane:        result.Active.Pane,
			Path:        result.Active.Path,
		},
	}, nil
}

func (b *TmuxBackend) Apply(ctx context.Context, actions []backend.Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := dryRunArgs(actions); err != nil {
		return err
	}
	created := make(map[string]*createdTarget)
	for i, action := range actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		actionCtx, cancel := context.WithTimeout(ctx, mutationTimeout)
		err := b.applyAction(actionCtx, action, created)
		cancel()
		if err != nil {
			return fmt.Errorf("action %d failed: %w", i, err)
		}
	}
	return nil
}

func (b *TmuxBackend) DryRun(actions []backend.Action) ([]string, error) {
	args, err := dryRunArgs(actions)
	if err != nil {
		return nil, err
	}
	lines := make([]string, len(args))
	for i := range args {
		lines[i] = "tmux " + strings.Join(args[i], " ")
	}
	return lines, nil
}

func (b *TmuxBackend) Attach(ctx context.Context, session string) error {
	return b.switchTo(ctx, session)
}

func (b *TmuxBackend) Switch(ctx context.Context, target backend.Target) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateTarget(target); err != nil {
		return err
	}
	if target.WindowID == "" {
		return b.switchTo(ctx, target.SessionID)
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	state, err := RunQuery(queryCtx, b.client, LoadStateQuery{})
	cancel()
	if err != nil {
		return err
	}

	resolved, err := resolveSwitchTarget(state.Sessions, target)
	if err != nil {
		return err
	}
	return b.switchTo(ctx, resolved)
}

func (b *TmuxBackend) switchTo(ctx context.Context, target string) error {
	if isInsideTmux() {
		return b.client.Execute(ctx, SwitchClient{Target: target})
	}
	return b.client.Execute(ctx, AttachSession{Target: target})
}

func isInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

func isEmptyServerError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no server running") || strings.Contains(message, "no current target")
}

type createdTarget struct {
	name     string
	windowID string
	paneIDs  []string
}

func (b *TmuxBackend) applyAction(ctx context.Context, a backend.Action, created map[string]*createdTarget) error {
	switch action := a.(type) {
	case backend.CreateSessionAction:
		target, err := b.createTarget(ctx, CreateSession{Name: action.Name, WindowName: action.WindowName, Path: action.Path}, action.WindowName)
		if err == nil {
			created[action.Name] = target
		}
		return err
	case backend.CreateWindowAction:
		target, err := b.createTarget(ctx, CreateWindow{Session: action.Session, Name: action.Name, Path: action.Path}, action.Name)
		if err == nil {
			created[action.Session] = target
		}
		return err
	case backend.SplitPaneAction:
		return b.applySplitPane(ctx, action, created)
	case backend.SendKeysAction:
		paneID, err := followupPaneID(created, action.Session, action.Window, action.Pane)
		if err != nil {
			return err
		}
		return b.client.Execute(ctx, SendKeys{Target: paneID, Keys: action.Command})
	case backend.SelectLayoutAction:
		target, err := followupTarget(created, action.Session, action.Window)
		if err != nil {
			return err
		}
		return b.client.Execute(ctx, SelectLayout{Target: target.windowID, Layout: action.Layout})
	case backend.ZoomPaneAction:
		paneID, err := followupPaneID(created, action.Session, action.Window, action.Pane)
		if err != nil {
			return err
		}
		return b.client.Execute(ctx, ZoomPane{Target: paneID})
	default:
		tmuxAction, err := staticAction(a)
		if err != nil {
			return err
		}
		return b.client.Execute(ctx, tmuxAction)
	}
}

func (b *TmuxBackend) applySplitPane(ctx context.Context, action backend.SplitPaneAction, created map[string]*createdTarget) error {
	target, err := followupTarget(created, action.Session, action.Window)
	if err != nil {
		return err
	}

	output, err := b.client.Run(ctx, printArgs(SplitPane{Target: target.windowID, Path: action.Path}, "#{pane_id}")...)
	if err != nil {
		return err
	}

	paneID := strings.TrimSpace(output)
	if err := format.ValidateObjectID("pane", paneID, '%'); err != nil {
		return fmt.Errorf("tmux returned %w", err)
	}

	target.paneIDs = append(target.paneIDs, paneID)
	return nil
}

func (b *TmuxBackend) createTarget(ctx context.Context, action Action, name string) (*createdTarget, error) {
	output, err := b.client.Run(ctx, printArgs(action, "#{window_id}|#{pane_id}")...)
	if err != nil {
		return nil, err
	}
	windowID, paneID, ok := strings.Cut(strings.TrimSpace(output), "|")
	if !ok {
		return nil, fmt.Errorf("tmux returned invalid created target %q", output)
	}
	if err := format.ValidateObjectID("window", windowID, '@'); err != nil {
		return nil, fmt.Errorf("tmux returned invalid created target %q: %w", output, err)
	}
	if err := format.ValidateObjectID("pane", paneID, '%'); err != nil {
		return nil, fmt.Errorf("tmux returned invalid created target %q: %w", output, err)
	}
	return &createdTarget{name: name, windowID: windowID, paneIDs: []string{paneID}}, nil
}

func followupTarget(created map[string]*createdTarget, session, window string) (*createdTarget, error) {
	target := created[session]
	if target == nil || target.name != window {
		return nil, fmt.Errorf("created window identity unavailable for %s:%s", session, window)
	}
	return target, nil
}

func followupPaneID(created map[string]*createdTarget, session, window string, pane int) (string, error) {
	target, err := followupTarget(created, session, window)
	if err != nil {
		return "", err
	}
	if pane < 0 || pane >= len(target.paneIDs) {
		return "", fmt.Errorf("created pane %d unavailable for %s:%s", pane, session, window)
	}
	return target.paneIDs[pane], nil
}

func printArgs(action Action, format string) []string {
	return append(action.Args(), "-P", "-F", format)
}

func staticAction(a backend.Action) (Action, error) {
	switch action := a.(type) {
	case backend.RenameSessionAction:
		return RenameSession{Target: action.Current, Name: action.New}, nil
	case backend.RenameWindowAction:
		if err := format.ValidateObjectID("window", action.WindowID, '@'); err != nil {
			return nil, err
		}
		return RenameWindow{Target: action.WindowID, Name: action.New}, nil
	case backend.KillSessionAction:
		return KillSession{Name: action.Name}, nil
	case backend.KillWindowAction:
		if err := format.ValidateObjectID("window", action.WindowID, '@'); err != nil {
			return nil, err
		}
		return KillWindow{Target: action.WindowID}, nil
	case backend.SetSessionOptionAction:
		return SetSessionOption{Session: action.Session, Key: action.Key, Value: action.Value}, nil
	default:
		return nil, fmt.Errorf("unsupported backend action %T", a)
	}
}

func dryRunArgs(actions []backend.Action) ([][]string, error) {
	result := make([][]string, 0, len(actions))
	created := make(map[string]*createdTarget)
	for i, action := range actions {
		if action == nil {
			return nil, fmt.Errorf("action %d is nil", i)
		}

		if err := action.Validate(); err != nil {
			return nil, fmt.Errorf("action %d is invalid: %w", i, err)
		}

		args, err := dryRunActionArgs(action, created)
		if err != nil {
			return nil, fmt.Errorf("action %d: %w", i, err)
		}

		result = append(result, args)
	}
	return result, nil
}

func dryRunActionArgs(a backend.Action, created map[string]*createdTarget) ([]string, error) {
	switch action := a.(type) {
	case backend.CreateSessionAction:
		args := printArgs(CreateSession{Name: action.Name, WindowName: action.WindowName, Path: action.Path}, "#{window_id}|#{pane_id}")
		created[action.Name] = dryRunTarget(action.Name, action.WindowName)
		return args, nil
	case backend.CreateWindowAction:
		args := printArgs(CreateWindow{Session: action.Session, Name: action.Name, Path: action.Path}, "#{window_id}|#{pane_id}")
		created[action.Session] = dryRunTarget(action.Session, action.Name)
		return args, nil
	case backend.SplitPaneAction:
		target, err := followupTarget(created, action.Session, action.Window)
		if err != nil {
			return nil, err
		}

		args := printArgs(SplitPane{Target: target.windowID, Path: action.Path}, "#{pane_id}")
		target.paneIDs = append(target.paneIDs, fmt.Sprintf("<new-pane:%s:%s:%d>", action.Session, action.Window, len(target.paneIDs)))
		return args, nil
	case backend.SendKeysAction:
		paneID, err := followupPaneID(created, action.Session, action.Window, action.Pane)
		if err != nil {
			return nil, err
		}
		return SendKeys{Target: paneID, Keys: action.Command}.Args(), nil
	case backend.SelectLayoutAction:
		target, err := followupTarget(created, action.Session, action.Window)
		if err != nil {
			return nil, err
		}
		return SelectLayout{Target: target.windowID, Layout: action.Layout}.Args(), nil
	case backend.ZoomPaneAction:
		paneID, err := followupPaneID(created, action.Session, action.Window, action.Pane)
		if err != nil {
			return nil, err
		}
		return ZoomPane{Target: paneID}.Args(), nil
	default:
		tmuxAction, err := staticAction(a)
		if err != nil {
			return nil, err
		}
		return tmuxAction.Args(), nil
	}
}

func dryRunTarget(session, window string) *createdTarget {
	return &createdTarget{
		name:     window,
		windowID: fmt.Sprintf("<new-window:%s:%s>", session, window),
		paneIDs:  []string{fmt.Sprintf("<new-pane:%s:%s:0>", session, window)},
	}
}
