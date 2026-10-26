package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

type Driver interface {
	Load(context.Context, ui.ViewMode) (list.Snapshot, error)
	Execute(context.Context, ui.ActionRequest) (ui.ActionResult, error)
	Preview(context.Context, ui.PreviewRequest) (string, error)
	Navigate(context.Context, list.ItemID) error
}

type RunUIFunc func(context.Context, list.Snapshot, ui.Options, ui.Runtime) (ui.Result, error)

type Service struct {
	Driver  Driver
	Options ui.Options
	RunUI   RunUIFunc
	// Notice is a terminal-safe application diagnostic for the initial snapshot only.
	Notice string
}

func NewService(detectBackend func() (Backend, error)) Service {
	return Service{
		Driver:  NewLiveAdapter(detectBackend),
		Options: ui.DefaultOptions(),
		RunUI:   ui.Run,
	}
}

func (s Service) Run(ctx context.Context) error {
	s, err := s.resolveOptions()
	if err != nil {
		return err
	}
	effectsCtx, cancelEffects := context.WithCancel(ctx)
	defer cancelEffects()
	effects := uiEffects{ctx: effectsCtx, cancel: cancelEffects, driver: s.Driver}

	initial, err := s.Driver.Load(effectsCtx, s.Options.Initial.View)
	if err != nil {
		return err
	}
	if s.Notice != "" {
		initial.Notice = strings.TrimSpace(s.Notice + "\n" + initial.Notice)
	}

	delivery, statusErr := openStatus(effectsCtx, s.Driver)
	if effectsCtx.Err() != nil {
		return effectsCtx.Err()
	}
	effects.status = delivery
	if statusErr != nil {
		initial.Notice = strings.TrimSpace(initial.Notice + "\nstatus unavailable: " + statusErr.Error())
	}
	runtime := ui.Runtime{Dispatch: effects.dispatch, Preview: effects.preview, Status: effects.statusOptions()}
	result, err := s.RunUI(effectsCtx, initial, s.Options, runtime)
	effects.stop()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if isCleanTUIExit(err) {
			return nil
		}
		return err
	}
	if !result.Navigation.IsZero() {
		return s.Driver.Navigate(ctx, result.Navigation)
	}
	return nil
}

// isCleanTUIExit accepts only an interrupt or a bare kill, because Bubble Tea
// also wraps panics and input failures in ErrProgramKilled.
func isCleanTUIExit(err error) bool {
	return errors.Is(err, tea.ErrInterrupted) || err == tea.ErrProgramKilled //nolint:errorlint // Wrapped kills carry a real cause.
}

func openStatus(ctx context.Context, driver Driver) (*statusDelivery, error) {
	if provider, ok := driver.(interface {
		OpenStatus(context.Context) (*statusDelivery, error)
	}); ok {
		return provider.OpenStatus(ctx)
	}
	return nil, nil
}

func (s Service) resolveOptions() (Service, error) {
	if s.Driver == nil {
		return s, fmt.Errorf("tui driver is not configured")
	}
	if s.RunUI == nil {
		s.RunUI = ui.Run
	}
	return s, s.Options.Validate()
}
