package list

import "context"

// Prepare evaluates a group of list changes on an isolated copy. The callback
// runs synchronously; callers may schedule it on their owned worker. Borrowed
// snapshots/details remain immutable. Cancellation or failure publishes no model.
// The callback must use list methods, and must not retain the supplied pointer.
func (m Model) Prepare(ctx context.Context, update func(*Model) error) (Model, error) {
	if err := ctx.Err(); err != nil {
		return Model{}, err
	}
	deferred := m.deferred
	m.workContext, m.deferred, m.rowHeight = ctx, false, nil
	// An independent identity also rejects results from the source list.
	m.searchKey.owner = new(byte)
	if err := update(&m); err != nil {
		return Model{}, err
	}
	if err := ctx.Err(); err != nil {
		return Model{}, err
	}
	m.workContext, m.deferred = nil, deferred
	return m, nil
}

func (m Model) operationContext() context.Context {
	if m.workContext != nil {
		return m.workContext
	}
	return context.Background()
}
