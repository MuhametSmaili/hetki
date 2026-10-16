package cmd

import (
	"context"

	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/logger"
)

func reportCommandError(ctx context.Context, err error) {
	if err == nil || (ctx.Err() == context.Canceled && backend.IsCancellationOnly(err)) {
		return
	}

	logger.Error("Error: %v", err)
	if hint := appshared.ErrorHint(err); hint != "" {
		logger.Plain("Hint: %s", hint)
	}
}
