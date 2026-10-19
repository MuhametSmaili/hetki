package start

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/converter"
	"github.com/MuhametSmaili/hetki/internal/logger"
	"github.com/MuhametSmaili/hetki/internal/manifest"
	"github.com/MuhametSmaili/hetki/internal/plan"
	"github.com/MuhametSmaili/hetki/internal/state"
)

type Options struct {
	// Nil selectors are omitted; non-nil empty values are invalid.
	Name   *string
	Path   *string
	DryRun bool
	Force  bool
}

// Backend provides the reconciliation and attachment operations used by starting.
type Backend interface {
	QueryState(context.Context) (backend.StateResult, error)
	LayoutsMatch(desired, actual string) bool
	Apply(context.Context, []backend.Action) error
	DryRun([]backend.Action) ([]string, error)
	Attach(context.Context, string) error
}

type Service struct {
	DetectBackend  func() (Backend, error)
	validateLayout func(string) error
	LoadWorkspace  func(string) (*manifest.Workspace, string, error)
}

// NewService binds backend detection and a required pure authored-layout policy.
func NewService(detectBackend func() (Backend, error), validateLayout func(string) error) Service {
	return Service{DetectBackend: detectBackend, validateLayout: validateLayout}
}

func (s Service) Run(ctx context.Context, opts Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.validateLayout == nil {
		return manifest.ErrLayoutValidatorRequired
	}

	path, err := appshared.ResolveWorkspaceTarget(opts.Name, opts.Path)
	if err != nil {
		return appshared.WithHint(err, "Use hetki start -n your_workspace or hetki start -p ./path/to/your_workspace.yaml. Replace the example name or path with your own.")
	}

	workspace, workspacePath, err := s.loadWorkspace(path)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b, err := s.detectBackend()
	if err != nil {
		return appshared.WithHint(fmt.Errorf("failed to detect backend: %w", err), "Install tmux and make sure it is available on PATH.")
	}

	p, err := buildPlan(ctx, b, workspace, opts.Force)
	if err != nil {
		return err
	}

	return executePlan(ctx, b, p, workspace, workspacePath, opts.DryRun)
}

func (s Service) loadWorkspace(path string) (*manifest.Workspace, string, error) {
	if s.LoadWorkspace != nil {
		return s.LoadWorkspace(path)
	}
	workspace, err := manifest.NewFileLoader(path, s.validateLayout).Load()
	if err != nil {
		return nil, "", appshared.WithHint(fmt.Errorf("loading workspace: %w", err), "Check that the selected file exists, is readable, and contains valid workspace YAML.")
	}
	return workspace, path, nil
}

func (s Service) detectBackend() (Backend, error) {
	if s.DetectBackend != nil {
		return s.DetectBackend()
	}
	return backend.Detect()
}

func buildPlan(ctx context.Context, b Backend, workspace *manifest.Workspace, force bool) (*plan.Plan, error) {
	desired := converter.ManifestToState(workspace)

	result, err := b.QueryState(ctx)
	if err != nil {
		return nil, appshared.WithHint(fmt.Errorf("failed to query backend state: %w", err), "Check access to the tmux server and retry.")
	}
	actual := converter.BackendResultToState(result)

	diff := state.Compare(desired, actual, b.LayoutsMatch)
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

func executePlan(ctx context.Context, b Backend, p *plan.Plan, workspace *manifest.Workspace, workspacePath string, dryRun bool) error {
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
		return appshared.WithHint(fmt.Errorf("failed to execute plan: %w", err), "Earlier actions are not rolled back. Inspect tmux state and use --dry-run before retrying.")
	}

	warnIfMetadataStampFails(ctx, b, workspace, workspacePath)

	return attachToSession(ctx, b, workspace)
}

func warnIfMetadataStampFails(ctx context.Context, b Backend, workspace *manifest.Workspace, workspacePath string) {
	err := stampWorkspacePath(ctx, b, workspace, workspacePath)
	if err == nil || (ctx.Err() == context.Canceled && backend.IsCancellationOnly(err)) {
		return
	}
	logger.Warning("workspace metadata not set: %v", err)
}

func stampWorkspacePath(ctx context.Context, b Backend, workspace *manifest.Workspace, workspacePath string) error {
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

func printDryRun(b Backend, actions []backend.Action) error {
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

func attachToSession(ctx context.Context, b Backend, workspace *manifest.Workspace) error {
	if len(workspace.Sessions) > 0 {
		return b.Attach(ctx, workspace.Sessions[0].Name)
	}
	return nil
}
