package converter

import (
	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/state"
)

func BackendResultToState(result backend.StateResult) *state.State {
	s := state.NewState()
	for _, sess := range result.Sessions {
		session := s.AddSession(sess.Name)
		session.ID = sess.ID
		for _, w := range sess.Windows {
			session.Windows = append(session.Windows, backendWindowToState(w))
		}
	}
	return s
}

func backendWindowToState(w backend.Window) *state.Window {
	window := &state.Window{ID: w.ID, Name: w.Name, Path: w.Path, Layout: w.Layout}
	for _, p := range w.Panes {
		window.Panes = append(window.Panes, &state.Pane{ID: p.ID, Index: p.Index, Path: p.Path, Command: p.Command, Zoom: p.Zoom})
	}
	return window
}
