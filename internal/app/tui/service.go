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
	Load(context.Context) (list.Snapshot, error)
	Execute(context.Context, ui.ActionRequest) (ui.ActionResult, error)
	Preview(context.Context, list.ItemID) (string, error)
	Navigate(context.Context, list.ItemID) error
}

type RunUIFunc func(context.Context, list.Snapshot, ui.KeyMap, ui.StartMode, ui.DispatchFunc, ui.PreviewOptions, ui.StatusOptions) (list.ItemID, error)

type Service struct {
	Driver       Driver
	Keys         ui.KeyMap
	StartMode    ui.StartMode
	PreviewWidth int
	RunUI        RunUIFunc
	StatusReader StatusReader
}

func NewService(detectBackend func() (Backend, error)) Service {
	return Service{
		Driver:    NewLiveAdapter(detectBackend),
		Keys:      ui.DefaultKeyMap(),
		StartMode: ui.DefaultStartMode(),
		RunUI:     ui.Run,
	}
}

func (s Service) Run(ctx context.Context) error {
	s, err := s.resolveOptions()
	if err != nil {
		return err
	}
	effectsCtx, cancelEffects := context.WithCancel(ctx)
	defer cancelEffects()
	effects := uiEffects{ctx: effectsCtx, cancel: cancelEffects, driver: s.Driver, reader: s.StatusReader}

	initial, err := s.Driver.Load(effectsCtx)
	if err != nil {
		return err
	}
	if effects.reader == nil {
		reader, statusErr := openStatus(effectsCtx, s.Driver)
		if effectsCtx.Err() != nil {
			return effectsCtx.Err()
		}
		effects.reader = reader
		if statusErr != nil {
			initial.Notice = strings.TrimSpace(initial.Notice + "\nstatus unavailable: " + statusErr.Error())
		}
	}
	preview := ui.PreviewOptions{Width: s.PreviewWidth, Read: effects.preview}
	navigation, err := s.RunUI(effectsCtx, initial, s.Keys, s.StartMode, effects.dispatch, preview, effects.statusOptions())
	effects.stop()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, tea.ErrInterrupted) {
			return nil
		}
		return err
	}
	if !navigation.IsZero() {
		return s.Driver.Navigate(ctx, navigation)
	}
	return nil
}

func openStatus(ctx context.Context, driver Driver) (StatusReader, error) {
	if provider, ok := driver.(interface {
		OpenStatus(context.Context) (StatusReader, error)
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
	if s.Keys.IsZero() {
		s.Keys = ui.DefaultKeyMap()
	}
	if s.StartMode == "" {
		s.StartMode = ui.DefaultStartMode()
	}
	if s.PreviewWidth == 0 {
		s.PreviewWidth = ui.DefaultPreviewWidth
	}
	if s.PreviewWidth < 1 || s.PreviewWidth > 99 {
		return s, fmt.Errorf("preview width must be between 1 and 99 percent")
	}
	return s, nil
}
