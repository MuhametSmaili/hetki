package tmux

import (
	"os"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/internal/format"
)

func invocationContext() *backend.Invocation {
	pane := os.Getenv("TMUX_PANE")
	if !isInsideTmux() && pane == "" {
		return nil
	}

	session := getCurrentSessionID()
	if format.ValidateObjectID("invoking session", session, '$') != nil {
		return &backend.Invocation{}
	}
	if pane != "" && format.ValidateObjectID("invoking pane", pane, '%') != nil {
		return &backend.Invocation{}
	}
	return &backend.Invocation{SessionID: session, PaneID: pane}
}
