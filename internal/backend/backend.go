package backend

import "context"

type Action interface {
	Comment() string
	Validate() error
}

type Backend interface {
	Name() string

	// Layout operations are pure and on demand; QueryState keeps observations opaque.
	LayoutsMatch(desired, actual string) bool
	ValidateObservedLayout(observed string) error
	LayoutPreset(observed string) (string, error)

	QueryState(context.Context) (StateResult, error)
	CapturePane(context.Context, Target) (string, error)
	Apply(context.Context, []Action) error
	DryRun([]Action) ([]string, error)
	Attach(context.Context, string) error
	Switch(context.Context, Target) error
}
