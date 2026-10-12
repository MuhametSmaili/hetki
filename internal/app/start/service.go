package start

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	appshared "github.com/MSmaili/hetki/internal/app"
	"github.com/MSmaili/hetki/internal/backend"
	"github.com/MSmaili/hetki/internal/converter"
	"github.com/MSmaili/hetki/internal/logger"
	"github.com/MSmaili/hetki/internal/manifest"
	"github.com/MSmaili/hetki/internal/plan"
	"github.com/MSmaili/hetki/internal/state"
)

type Options struct {
	Workspace string
	DryRun    bool
	Force     bool
}

type Service struct {
	DetectBackend func(...string) (backend.Backend, error)
	LoadWorkspace func(string) (*manifest.Workspace, string, error)
}

func NewService(detectBackend func(...string) (backend.Backend, error)) Service {
	return Service{DetectBackend: detectBackend}
}

func (s Service) Run(ctx context.Context, opts Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	workspace, workspacePath, err := s.loadWorkspace(opts.Workspace)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b, err := s.detectBackend()
	if err != nil {
		return fmt.Errorf("failed to detect backend: %w", err)
	}

	p, err := buildPlan(ctx, b, workspace, opts.Force)
	if err != nil {
		return err
	}

	return executePlan(ctx, b, p, workspace, workspacePath, opts.DryRun)
}

func (s Service) loadWorkspace(nameOrPath string) (*manifest.Workspace, string, error) {
	if s.LoadWorkspace != nil {
		return s.LoadWorkspace(nameOrPath)
	}
	return appshared.NewWorkspaceLoader().LoadWorkspace(nameOrPath)
}

func (s Service) detectBackend() (backend.Backend, error) {
	if s.DetectBackend != nil {
		return s.DetectBackend()
	}
	return backend.Detect()
}

func buildPlan(ctx context.Context, b backend.Backend, workspace *manifest.Workspace, force bool) (*plan.Plan, error) {
	desired := converter.ManifestToState(workspace)

	result, err := b.QueryState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query backend state: %w\nHint: Verify tmux is running and retry", err)
	}
	actual := converter.BackendResultToState(result)

	diff := state.Compare(desired, actual)
	planDiff := converter.StateDiffToPlanDiff(diff, desired)

	strategy := selectStrategy(force)
	p := strategy.Plan(planDiff)
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid reconciliation plan: %w", err)
	}
	return p, nil
}

func selectStrategy(force bool) plan.Strategy {
	if force {
		return &plan.ForceStrategy{}
	}
	return &plan.MergeStrategy{}
}

func executePlan(ctx context.Context, b backend.Backend, p *plan.Plan, workspace *manifest.Workspace, workspacePath string, dryRun bool) error {
	if p.IsEmpty() {
		logger.Info("Workspace already up to date")
		if dryRun {
			return nil
		}

		warnIfMetadataStampFails(ctx, b, workspace, workspacePath)
		return attachToSession(ctx, b, workspace)
	}

	actions, err := toBackendActions(p.Actions)
	if err != nil {
		return fmt.Errorf("failed to convert plan: %w", err)
	}
	if dryRun {
		return printDryRun(b, actions)
	}

	if err := b.Apply(ctx, actions); err != nil {
		return fmt.Errorf("failed to execute plan: %w\nHint: Check tmux server logs or try with --dry-run to see planned actions", err)
	}

	warnIfMetadataStampFails(ctx, b, workspace, workspacePath)

	return attachToSession(ctx, b, workspace)
}

func warnIfMetadataStampFails(ctx context.Context, b backend.Backend, workspace *manifest.Workspace, workspacePath string) {
	if err := stampWorkspacePath(ctx, b, workspace, workspacePath); err != nil {
		logger.Warning("workspace metadata not set: %v", err)
	}
}

func stampWorkspacePath(ctx context.Context, b backend.Backend, workspace *manifest.Workspace, workspacePath string) error {
	path := strings.TrimSpace(workspacePath)
	if path == "" {
		return nil
	}
	if absPath, err := filepath.Abs(path); err == nil {
		path = absPath
	}

	actions := make([]backend.Action, 0, len(workspace.Sessions))
	for _, session := range workspace.Sessions {
		if strings.TrimSpace(session.Name) == "" {
			continue
		}
		actions = append(actions, backend.SetSessionOptionAction{
			Session: session.Name,
			Key:     backend.WorkspacePathOption,
			Value:   path,
		})
	}
	if len(actions) == 0 {
		return nil
	}
	if err := b.Apply(ctx, actions); err != nil {
		return err
	}
	return nil
}

func printDryRun(b backend.Backend, actions []backend.Action) error {
	lines, err := b.DryRun(actions)
	if err != nil {
		return fmt.Errorf("failed to render dry run: %w", err)
	}
	logger.Info("Dry run - actions to execute:")
	for _, line := range lines {
		logger.Plain("  %s", line)
	}
	return nil
}

func toBackendActions(actions []plan.Action) ([]backend.Action, error) {
	result := make([]backend.Action, len(actions))
	for i, action := range actions {
		if action == nil {
			return nil, fmt.Errorf("action %d is nil", i)
		}
		if err := action.Validate(); err != nil {
			return nil, fmt.Errorf("action %d is invalid: %w", i, err)
		}
		converted, err := toBackendAction(action)
		if err != nil {
			return nil, fmt.Errorf("action %d: %w", i, err)
		}
		result[i] = converted
	}
	return result, nil
}

func toBackendAction(action plan.Action) (backend.Action, error) {
	switch a := action.(type) {
	case plan.CreateSessionAction:
		return backend.CreateSessionAction{Name: a.Name, WindowName: a.WindowName, Path: a.Path}, nil
	case plan.CreateWindowAction:
		return backend.CreateWindowAction{Session: a.Session, Name: a.Name, Path: a.Path}, nil
	case plan.SplitPaneAction:
		return backend.SplitPaneAction{Session: a.Session, Window: a.Window, Path: a.Path}, nil
	case plan.SendKeysAction:
		return backend.SendKeysAction{Session: a.Session, Window: a.Window, Pane: a.Pane, Command: a.Command}, nil
	case plan.KillSessionAction:
		return backend.KillSessionAction{Name: a.Name}, nil
	case plan.KillWindowAction:
		return backend.KillWindowAction{Session: a.Session, Window: a.Window, WindowID: a.WindowID}, nil
	case plan.SelectLayoutAction:
		return backend.SelectLayoutAction{Session: a.Session, Window: a.Window, Layout: a.Layout}, nil
	case plan.ZoomPaneAction:
		return backend.ZoomPaneAction{Session: a.Session, Window: a.Window, Pane: a.Pane}, nil
	default:
		return nil, fmt.Errorf("unsupported plan action %T", action)
	}
}

func attachToSession(ctx context.Context, b backend.Backend, workspace *manifest.Workspace) error {
	if len(workspace.Sessions) > 0 {
		return b.Attach(ctx, workspace.Sessions[0].Name)
	}
	return nil
}
