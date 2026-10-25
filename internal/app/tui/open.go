package tui

import (
	"context"
	"errors"

	"github.com/MuhametSmaili/hetki/internal/config"
	"github.com/MuhametSmaili/hetki/internal/terminal"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
)

// Open reads startup preferences once before constructing the picker service.
// Direct Service users remain independent of personal configuration files.
func Open(ctx context.Context, detectBackend func() (Backend, error), overrides Overrides) error {
	service, err := prepareService(ctx, detectBackend, overrides)
	if err == nil {
		err = service.Run(ctx)
	}
	if err != nil {
		return &diagnosticError{cause: err}
	}
	return nil
}

func prepareService(ctx context.Context, detectBackend func() (Backend, error), overrides Overrides) (Service, error) {
	if err := ctx.Err(); err != nil {
		return Service{}, err
	}
	if err := overrides.Validate(); err != nil {
		return Service{}, err
	}

	preferences, loadErr := loadConfig(ctx)
	if err := ctx.Err(); err != nil {
		return Service{}, err
	}
	if errors.Is(loadErr, context.Canceled) || errors.Is(loadErr, context.DeadlineExceeded) {
		return Service{}, loadErr
	}
	if loadErr != nil {
		preferences = config.Config{}
	}

	options, warning := resolveConfig(preferences)
	options = overrides.apply(options)
	if loadErr != nil {
		warning = "configuration ignored; using defaults except for explicit flags: " + loadErr.Error()
	} else if warning != "" {
		warning = "configuration warning: " + warning
	}
	if err := ctx.Err(); err != nil {
		return Service{}, err
	}

	return Service{Driver: NewLiveAdapter(detectBackend), Options: options, RunUI: ui.Run, Notice: configNotice(warning)}, nil
}

func loadConfig(ctx context.Context) (config.Config, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Config{}, err
	}
	return config.LoadFile(ctx, path)
}

func configNotice(warning string) string {
	// Bound bytes too: zero-width text can evade a display-width limit.
	const maxSourceBytes = 1024
	if len(warning) > maxSourceBytes {
		warning = warning[:maxSourceBytes] + "..."
	}
	return terminal.Truncate(terminal.Sanitize(warning), 512)
}

// Keep errors.Is/As usable while preventing paths and causes from injecting
// terminal control sequences into the command's diagnostic output.
type diagnosticError struct{ cause error }

func (e *diagnosticError) Error() string { return terminal.Sanitize(e.cause.Error()) }
func (e *diagnosticError) Unwrap() error { return e.cause }
