package tui

import (
	"context"

	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

func newModel(snapshot list.Snapshot, dispatch DispatchFunc) model {
	m, err := newModelWithKeys(snapshot, dispatch, DefaultKeyMap())
	if err != nil {
		return model{err: err, dispatch: dispatch, keys: DefaultKeyMap(), theme: defaultTheme()}
	}
	return m
}

func newModelWithKeys(snapshot list.Snapshot, dispatch DispatchFunc, keys KeyMap) (model, error) {
	return newModelWithStartMode(snapshot, dispatch, keys, DefaultStartMode())
}

func newModelWithStartMode(snapshot list.Snapshot, dispatch DispatchFunc, keys KeyMap, startMode StartMode) (model, error) {
	options := DefaultOptions()
	options.Keys, options.StartMode = keys, startMode
	return newModelWithOptions(context.Background(), snapshot, options, Runtime{Dispatch: dispatch})
}
