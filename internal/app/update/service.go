package update

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/MuhametSmaili/hetki/internal/logger"
)

const (
	modulePath = "github.com/MuhametSmaili/hetki"
	githubRepo = "MuhametSmaili/hetki"
)

type Options struct {
	CurrentVersion string
	CurrentCommit  string
	TargetVersion  string // exact tag from --version; empty means latest
	Head           bool   // build the latest commit on main
	DryRun         bool
	Verbose        bool
}

type Target struct {
	Tag    string // empty for a commit-pinned source build; filled after module resolution
	Commit string
}

type Updater interface {
	Name() string
	Update(context.Context, Target) error
	DryRun(Target)
}

type Service struct {
	SetVerbose       func(bool)
	Executable       func() (string, error)
	DetermineUpdater func(string) (Updater, error)
	ResolveTarget    func(context.Context, Options) (string, error)
	ResolveCommit    func(context.Context, string) (string, error)
	ResolveHead      func(context.Context) (string, error)
}

func NewService() Service {
	return Service{}
}

func (s Service) Run(ctx context.Context, opts Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.Head && opts.TargetVersion != "" {
		return errors.New("--head cannot be combined with --version")
	}

	s.setVerbose(opts.Verbose)

	exePath, err := s.executable()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}
	logger.Debug("Executable path: %s", exePath)

	updater, err := s.determineUpdater(ctx, exePath, opts)
	if err != nil {
		return err
	}
	logger.Verbose("Detected installation method: %s", updater.Name())

	if opts.Head {
		return s.runHeadUpdate(ctx, opts, updater)
	}

	return s.runReleaseUpdate(ctx, opts, updater)
}

func (s Service) runReleaseUpdate(ctx context.Context, opts Options, updater Updater) error {
	// D4: fail closed — no release update proceeds without an exact resolved tag.
	targetTag, err := s.resolveTarget(ctx, opts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("could not resolve a release to install: %w", err)
	}
	exact := opts.TargetVersion != ""
	install, reason, err := decideUpdate(opts.CurrentVersion, targetTag, exact)
	if err != nil {
		return err
	}
	if !install {
		logger.Success("%s (%s)", reason, opts.CurrentVersion)
		return nil
	}

	commit, err := s.resolveCommit(ctx, targetTag)
	if err != nil {
		return fmt.Errorf("could not resolve commit for %s: %w", targetTag, err)
	}
	target := Target{Tag: targetTag, Commit: commit}
	if opts.DryRun {
		updater.DryRun(target)
		return nil
	}
	if opts.CurrentVersion == "dev" {
		logger.Info("Development build detected, updating to: %s", targetTag)
	} else {
		logger.Info("Current version: %s", opts.CurrentVersion)
		logger.Info("Updating to: %s (%s)", targetTag, reason)
	}
	if err := updater.Update(ctx, target); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	logger.Success("Update completed successfully")
	return nil
}

func (s Service) runHeadUpdate(ctx context.Context, opts Options, updater Updater) error {
	commit, err := s.resolveHead(ctx)
	if err != nil {
		return fmt.Errorf("could not resolve the latest main commit: %w", err)
	}
	if !validCommit(commit) {
		return errors.New("main does not resolve to a valid commit")
	}
	if commit == opts.CurrentCommit {
		logger.Success("already on the latest main commit (%s)", commit[:12])
		return nil
	}
	target := Target{Commit: commit}
	if opts.DryRun {
		updater.DryRun(target)
		return nil
	}
	logger.Info("Current version: %s", opts.CurrentVersion)
	logger.Info("Updating to latest main commit: %s", commit[:12])
	if err := updater.Update(ctx, target); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	logger.Success("Update completed successfully")
	return nil
}

func (s Service) setVerbose(verbose bool) {
	if s.SetVerbose != nil {
		s.SetVerbose(verbose)
		return
	}
	logger.SetVerbose(verbose)
}

func (s Service) executable() (string, error) {
	if s.Executable != nil {
		return s.Executable()
	}
	return os.Executable()
}

func (s Service) determineUpdater(ctx context.Context, exePath string, opts Options) (Updater, error) {
	if s.DetermineUpdater != nil {
		return s.DetermineUpdater(exePath)
	}
	return DetermineUpdater(ctx, exePath, opts)
}

func (s Service) resolveTarget(ctx context.Context, opts Options) (string, error) {
	if s.ResolveTarget != nil {
		return s.ResolveTarget(ctx, opts)
	}
	return ResolveTarget(ctx, opts)
}

func (s Service) resolveCommit(ctx context.Context, tag string) (string, error) {
	if s.ResolveCommit != nil {
		return s.ResolveCommit(ctx, tag)
	}
	return resolveTagCommit(ctx, tag)
}

func (s Service) resolveHead(ctx context.Context) (string, error) {
	if s.ResolveHead != nil {
		return s.ResolveHead(ctx)
	}
	return resolveHeadCommit(ctx)
}
