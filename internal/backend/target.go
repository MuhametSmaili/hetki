package backend

// Target identifies a session, window occurrence, or pane occurrence using IDs
// returned by QueryState. IDs are opaque to callers. A window requires its
// session; a pane requires both its window and session. The zero value is absent.
// Navigation selects the identified object; session/window capture follows its
// current active pane. Native validation and targeting belong to the backend.
type Target struct {
	SessionID string
	WindowID  string
	PaneID    string
}
