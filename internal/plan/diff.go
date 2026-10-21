package plan

type Diff struct {
	Sessions ItemDiff[Session]
	Windows  map[string]ItemDiff[Window] // key: session|name
}

type ItemDiff[T any] struct {
	Missing    []T
	Extra      []T
	Mismatched []Mismatch[T]
}

type Mismatch[T any] struct {
	Desired T
	Actual  T
}

type Session struct {
	ID      string
	Name    string
	Windows []Window
}

type Window struct {
	ID     string
	Name   string
	Path   string
	Layout string
	Panes  []Pane
}

type Pane struct {
	Path    string
	Command string
	Zoom    bool
}
