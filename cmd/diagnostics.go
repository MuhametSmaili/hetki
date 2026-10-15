package cmd

import (
	appshared "github.com/MuhametSmaili/hetki/internal/app"
	"github.com/MuhametSmaili/hetki/internal/logger"
)

func reportCommandError(err error) {
	if err == nil {
		return
	}

	logger.Error("Error: %v", err)
	if hint := appshared.ErrorHint(err); hint != "" {
		logger.Plain("Hint: %s", hint)
	}
}
